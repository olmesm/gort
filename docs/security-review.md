# Security controls and test coverage

[Back to the README](../README.md) · [Deployment](deployment.md)

The application uses FastAPI, Pydantic, PostgreSQL, SQLModel/SQLAlchemy,
Alembic and Jinja. These notes describe the current implementation and tests;
they are not a production infrastructure audit.

| Area | Implementation | Checks |
|---|---|---|
| Sessions and API keys | `gort/auth.py` signs cookies, reloads user roles, invalidates local sessions after password changes and checks key expiry. | Authentication tests cover tampering, revocation, expiry, unknown roles and group restrictions. |
| OIDC | `gort/ui.py` uses PKCE, nonce and signed ID-token validation. Sessions end by token expiry. | A signed RSA mock provider exercises discovery, code exchange, JWKS verification, provisioning and callback replay rejection. |
| Browser and proxy protection | `gort/middleware.py` checks form origins and trusted proxy hops, limits bodies and applies security headers and per-IP rate limits. | Public-route tests cover rejected cross-origin forms, headers and untrusted forwarded IPs. |
| Outbound HTTP | `gort/outbound.py` checks DNS answers and connects to the checked address, including on redirects. | Worker tests cover blocked addresses, mixed DNS answers, connection pinning and persistent webhook retries. |
| Persistence and permissions | Parameterized PostgreSQL queries and transactions back the shared application operations. Alembic manages the schema. | Database, REST and GraphQL tests exercise real PostgreSQL in isolated schemas, including partial updates and scoped access. |
| HTML rendering | Jinja autoescapes dashboard pages, htmx fragments and chart templates. | Escaping, template compilation and browser tests cover forms, charts and responsive layouts. |

The dashboard uses `Cache-Control: no-store`. Responses include MIME-sniffing,
framing and referrer protections. Set `GORT_USE_HTTPS=true` to mark session
cookies Secure, and configure only the actual proxy CIDRs as trusted.

Passwords use bcrypt. Password input over bcrypt's 72-byte UTF-8 limit is rejected.
API keys are hashed in PostgreSQL. Signing keys are stored under a private data
directory. Webhook secrets remain readable to the application because it needs
them to sign outgoing deliveries; creation responses show each secret once.

Queries bind user-supplied values. Sort fields and analytics breakdown columns
come from allowlists. Link field and tag changes commit in the same transaction.
REST and GraphQL use the same API-key permissions. Dashboard groups are a
separate access model. Regular users cannot access global administration or
analytics pages.

## Limits

- Logout clears the browser cookie but does not revoke a stolen copy. Local
  sessions expire after at most 14 days; password changes and account deletion
  invalidate them earlier. Role changes take effect on the next request.
- OIDC group changes and identity-provider logout take effect at re-login or
  token expiry. The application does not use continuous introspection or
  back-channel logout.
- `GORT_ALLOW_PRIVATE_OUTBOUND=true` permits internal destinations for title
  requests and webhooks. The default blocks them.
- Rate limits apply per process and client IP. Multiple processes need shared
  limits at the reverse proxy if a single limit across them is required.
- Error logs do not provide a durable administrative audit trail or distributed
  account lockout.

## Run the checks

Set `GORT_TEST_POSTGRES_DSN` to a disposable PostgreSQL database and run
`make check`. Tests create and remove temporary schemas. Browser checks run
through `npm test` in `e2e/`. The [local identity-provider stack](../e2e/keycloak-smoke/README.md)
adds the live Keycloak login and group-access checks.
