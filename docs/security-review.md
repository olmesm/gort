# Security and Go architecture review

Reviewed 14 September 2026 against the working tree following v0.2.0. Three agents
reviewed SQL/authentication, browser/network behavior, and dependencies/Go patterns.
Findings were checked against code and local probes. This is a bounded engineering
review, not a penetration-test certification or a guarantee of no vulnerabilities.

## Findings addressed

| Area | Confirmed issue | Current behavior |
|---|---|---|
| Authorization | Author API keys could reuse another author's link through `findIfExists` | Reuse queries are scoped to the requesting author. REST and GraphQL share the fix. |
| Dashboard isolation | Regular users could inspect or mutate global tags and inspect global analytics/orphan traffic | Global pages and operations require admin. Regular users land on their scoped links. |
| Session authorization | Cookie roles outlived user deletion/demotion | Requests reload the user and role from the database. |
| Password sessions | Password reset left existing local cookies usable | Cookies are bound to an HMAC version of the stored password hash. Resetting it invalidates existing cookies. Legacy local cookies require login again. |
| OIDC | Sessions outlived verified token validity | OIDC cookies expire no later than the ID token. Legacy OIDC cookies require login. Password login cannot authenticate OIDC shadow accounts. OIDC HTTP calls time out after 15 seconds. |
| SSRF | Title resolution and webhook delivery could reach internal services, including through redirects | Each DNS result is checked, and the validated IP is dialed directly. Private, loopback, link-local, metadata and special-use addresses are denied by default. Environment proxies cannot bypass the check. |
| Proxy spoofing | Arbitrary forwarded IP headers bypassed IP rate limits | Only configured proxy CIDRs are trusted; trusted hops are stripped from the right. Password login attempts are also rate-limited. |
| Browser requests | Same-site sibling origins could submit dashboard forms | Standard-library cross-origin protection checks unsafe dashboard requests, including login/logout. |
| Redirects | Backslashes/control characters could change login return-URL interpretation | Those forms are rejected. |
| Errors | GraphQL exposed internal database errors and some page handlers discarded template failures | Internal failures return generic errors; template failures propagate. |
| Password handling | Bcrypt passwords over 72 bytes could panic or match a truncated prefix | Creation/reset/bootstrap report errors and login rejects overlong passwords. |
| Storage | New data paths used permissive defaults | New directories use 0700 and new SQLite files use 0600. Existing modes are preserved. |
| Downloads | GeoIP extraction had no decompressed-size bound | Total decompressed input is limited to 512 MiB, and regular database entries to 256 MiB. |
| Persistence | Link edits could partly commit before tag failure | Fields and tag replacement commit in one transaction. Failure-injection tests cover both databases. |

The browser middleware also sets no-store on dashboard responses, MIME-sniffing,
framing and referrer protections. Secure cookies require `GORT_USE_HTTPS=true`.

## SQL injection and template review

No exploitable SQL injection was found in the inspected paths. Values use bound
parameters, including lists expanded by `InList`; PostgreSQL placeholders are
rebound centrally. Sort direction/columns and analytics breakdown identifiers are
selected from allowlists. Tests send SQL-shaped values through search, tags,
groups, domains and sorting on SQLite and PostgreSQL.

The templates use `html/template` contextual escaping, embedded source files,
startup parsing, cached clones, layouts/partials and typed view models. The Go
[Clone documentation](https://pkg.go.dev/html/template#Template.Clone) describes
this shared-layout pattern. User strings are data, not template source. No
exploitable XSS was found in inspected templates, htmx fragments or QR output.
Chart SVG now lives in an ordinary partial with typed geometry instead of a
`template.HTML` string builder; bar percentages use numeric values.

## Remaining limitations and deployment requirements

- **Logout revocation:** logout clears the browser cookie but does not revoke a
  stolen copy of a local signed cookie. It remains valid until expiry, password
  reset, account deletion or loss of authorization. Local cookies last up to
  14 days. Server-side sessions would close this gap.
- **IdP revocation:** OIDC group changes and IdP logout are enforced on re-login or
  ID-token expiry, not through continuous introspection or back-channel logout.
- **Private outbound opt-in:** `GORT_ALLOW_PRIVATE_OUTBOUND=true` deliberately
  permits internal destinations. This applies to title resolution as well as
  admin-configured webhooks; keep it disabled when untrusted users can create links.
- **Proxy/HTTPS configuration:** trust only real proxy CIDRs and enable HTTPS
  cookies behind TLS termination. Existing data-file permissions need operator
  review; the app does not chmod existing shared paths.
- **Operational controls:** application errors are logged, but there is no durable
  administrative audit trail, account-based login lockout, or alerting pipeline.
  Per-IP limits do not prevent distributed credential attacks.
- **Deployment scope:** reverse proxies, production identity-provider policy,
  container/OS vulnerabilities and production network exposure were not exhaustively
  audited. Keycloak dev-mode credentials in the test stack are local fixtures.

These findings cover the relevant [OWASP Top 10:2025](https://top10.owasp.org/2025/)
areas: access control, configuration, supply chain, cryptography, injection,
design, authentication, integrity, logging and exceptional conditions.

## Tools and reproducible checks

- `govulncheck` v1.8.0 on Go 1.26.7, Darwin and Linux amd64: zero reachable or
  imported-package vulnerabilities. Updated `x/crypto` to v0.56.0. The remaining
  [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) advisory concerns the unused
  `openpgp` package inside that module; Gort does not import it.
- `gosec` v2.29.0 baseline: 18 warnings, not a clean scan. Confirmed filesystem
  permissions and decompression findings were fixed. Other baseline warnings
  concerned configuration-owned paths, conditional Secure cookies, numeric SVG,
  shutdown context, generic redirects and ignored cleanup/write errors.
- The Go integration harness accepts `GORT_TEST_POSTGRES_DSN` and creates an
  isolated schema per test. See [the local stack](../e2e/keycloak-smoke/README.md)
  for PostgreSQL 16 and Keycloak 26.7.3 browser tests.
- `.github/workflows/check.yml` adds routine test/race, vet and reachable-vulnerability
  checks. Its PostgreSQL job uses the same isolated-schema test harness.

Final validation on the resulting working tree:

- Go 1.26.7 race suite passed on SQLite and PostgreSQL 16; vet passed.
- All 31 dashboard browser cases passed across the main run and a targeted rerun
  after updating the regular-user landing-page assertion.
- All 20 real Keycloak 26.7.3/PostgreSQL browser checks passed.
- GraphQL regeneration completed without changing generated models or execution code.
- The chart partial rendered valid SVG in the running local instance.

## Libraries and patterns

Keep Chi, Huma, gqlgen, `database/sql`, `html/template` and `embed`. The remaining
cleanup consolidated visit predicates and tag mapping, rejected malformed form
expiry dates, and propagated documentation rendering failures.

[SCS](https://github.com/alexedwards/scs) is the best candidate for a future session
refactor: its server-side session store and `Destroy` operation can revoke copied
cookies at logout. It would require session-table migrations for both databases,
modernc SQLite compatibility tests and replacement of custom cookie handling.
Potential line savings are unmeasured, so no migration was made in this pass.

[sqlc](https://docs.sqlc.dev/en/latest/) was evaluated for fixed queries and then
removed. Its generated parameter types and schema checks did not offset the
additional handwritten adapters, SQL and configuration in this integration.
The repositories retain `database/sql`, bound parameters and shared query helpers.
[sqlx](https://jmoiron.github.io/sqlx/) was not added; reflection would not remove
the dynamic query construction or SQLite timestamp handling.

[templ](https://templ.guide/core-concepts/template-generation/) offers compile-time
component checking, with a compiler and generated Go files. That is a different
tradeoff, not a necessary correction to the existing idiomatic templates.
