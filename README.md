# Gort

A self-hosted URL shortener written in Go, built on the standard library's
`net/http` and [htmx](https://htmx.org/). Single binary, full REST API,
server-rendered admin dashboard, rich visit analytics.

Gort is a feature-for-feature Go port of
[Shortlink](https://github.com/olmesm/shortlink) (F#): same REST API surface,
same dashboard UI, same behavior — different runtime.

## Features

- **Short URLs** — auto-generated codes (configurable length) or custom slugs
  (including path-style slugs like `docs/intro`), URL validation, automatic page
  title resolution, tags, per-URL redirect status (301/302/307/308), optional
  query-string forwarding.
- **Multi-domain** — serve many domains from one instance; short codes are unique
  per domain; unknown domains auto-register on first use; per-domain not-found
  redirects.
- **Lifetime controls** — `validSince`, `validUntil` and `maxVisits`; expired or
  exhausted links fall back to the configured not-found behavior.
- **Conditional redirect rules** — per-URL rules evaluated top-down that override
  the target by device (Android/iOS/mobile/desktop), `Accept-Language`, query
  parameter, or visitor IP/CIDR.
- **Visit analytics** — every redirect records referrer, user agent (parsed to
  browser/OS), device, bot detection, and IP-derived geolocation via MaxMind
  GeoLite2 (auto-downloaded and refreshed when a license key is configured).
  IPs are anonymized by default; IP capture and tracking as a whole can be
  disabled. Orphan visits (base URL hits, unknown short codes, other 404s) are
  tracked separately.
- **REST API** — everything is scriptable under `/rest/v1` with API keys
  (admin / author / domain-scoped roles), RFC 7807 problem responses,
  pagination, search, ordering and rate limiting.
- **Admin dashboard** — served at `/admin`; multi-user (admin/user roles),
  cookie sessions, htmx-driven live search and pagination, server-rendered SVG
  charts, QR previews, redirect-rule builder. No JS build step.
- **OIDC single sign-on & link groups** — plug in Keycloak (or any compliant
  IdP): users are auto-provisioned on first login, the token's groups claim
  both scopes what each user can see (non-admins see ungrouped links plus
  links in their groups) and grants the admin role via a configurable admin
  group.
- **QR codes** — public `GET /{code}/qr-code` in PNG or SVG with size, margin
  and error-correction options.
- **Webhooks** — signed JSON POSTs (HMAC-SHA256) on `url.created`,
  `visit.recorded` and `orphan_visit.recorded`, delivered by a persistent queue
  with exponential-backoff retries.
- **robots.txt** — generated from per-URL `crawlable` flags.
- **SQLite or PostgreSQL** — SQLite by default (zero config, pure Go driver —
  no cgo), PostgreSQL for bigger installs. Schema migrates automatically on
  startup.

## Quick start

### Docker

```sh
docker compose up --build
```

Then open <http://localhost:8080/admin>. On first start an `admin` user is
created; its password comes from `GORT_INITIAL_ADMIN_PASSWORD`, or is
generated and printed in the container log.

For PostgreSQL:

```sh
docker compose --profile postgres up --build
```

and un-comment the `GORT_DB_*` variables in `docker-compose.yml`.

### From source

Requires Go 1.24+.

```sh
go run ./cmd/gort
```

Run the test suite (unit + full-stack integration tests):

```sh
go test ./...
```

Run the browser end-to-end tests (Playwright driving the real dashboard in
Chromium — 25 tests covering login, short URL lifecycle, htmx live search,
redirect rules, analytics, tags, domains, API keys, webhooks, users, link groups and
orphan visits; requires Node.js and Go):

```sh
cd e2e
npm install
npx playwright install chromium   # once; or set PLAYWRIGHT_CHROMIUM_PATH to an existing binary
npm test
```

The e2e config builds and starts the app itself on port 18100 with a
throw-away SQLite database, so no setup is needed.

## Configuration

Everything is configured through environment variables.

| Variable | Default | Purpose |
|---|---|---|
| `GORT_PORT` | `8080` | HTTP listen port |
| `GORT_DEFAULT_DOMAIN` | `localhost:<port>` | Authority used to build short URLs when no domain is given |
| `GORT_USE_HTTPS` | `false` | Render short URLs with `https://` |
| `GORT_DATA_DIR` | `./data` | SQLite db, GeoLite2 db, session signing keys |
| `GORT_DB_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `GORT_DB_CONNECTION` | SQLite in data dir | SQLite file path or PostgreSQL connection string |
| `GORT_SHORT_CODE_LENGTH` | `5` | Length of generated codes (min 4) |
| `GORT_REDIRECT_STATUS` | `302` | Default redirect status (301/302/307/308) |
| `GORT_AUTO_RESOLVE_TITLES` | `true` | Fetch page `<title>` in the background |
| `GORT_DISABLE_TRACKING` | `false` | Record no visits at all |
| `GORT_DISABLE_IP_TRACKING` | `false` | Track visits but never record IPs |
| `GORT_ANONYMIZE_IPS` | `true` | Zero the host bits of recorded IPs |
| `GORT_TRACK_SKIP_PARAM` | *(unset)* | Query param that opts a request out of tracking (e.g. `no-track`) |
| `GORT_TRACK_ORPHAN_VISITS` | `true` | Track base-URL/404 traffic |
| `GORT_BASE_URL_REDIRECT` | *(unset)* | Where `GET /` redirects (else a landing page) |
| `GORT_REGULAR_404_REDIRECT` | *(unset)* | Redirect for non-short-URL 404s |
| `GORT_INVALID_SHORT_URL_REDIRECT` | *(unset)* | Redirect for unknown/expired short codes |
| `GORT_GEOLITE_LICENSE_KEY` | *(unset)* | Enables GeoLite2 download + visit geolocation |
| `GORT_INITIAL_ADMIN_USERNAME` | `admin` | First-run dashboard admin |
| `GORT_INITIAL_ADMIN_PASSWORD` | *(generated)* | First-run admin password |
| `GORT_RATE_LIMIT_PER_MINUTE` | `120` | Mutating REST calls per minute per IP (0 disables) |
| `GORT_OIDC_ISSUER` | *(unset)* | Enables SSO; the IdP's issuer URL (e.g. `https://kc.example.com/realms/main`) |
| `GORT_OIDC_CLIENT_ID` | *(unset)* | OIDC client id (required with issuer) |
| `GORT_OIDC_CLIENT_SECRET` | *(unset)* | OIDC client secret (confidential clients) |
| `GORT_OIDC_REDIRECT_URL` | *(derived)* | Callback override; default `{scheme}://{host}/admin/oidc/callback` |
| `GORT_OIDC_SCOPES` | `profile email` | Extra scopes besides `openid` (space/comma separated) |
| `GORT_OIDC_GROUPS_CLAIM` | `groups` | Token claim carrying the user's groups |
| `GORT_OIDC_ADMIN_GROUP` | `gort-admins` | Members of this group become dashboard admins |
| `GORT_OIDC_PROVIDER_NAME` | `SSO` | Label on the login button |
| `GORT_OIDC_ONLY` | `false` | Hide local password login |

Per-domain not-found redirects (configured in the dashboard or via
`PATCH /rest/v1/domains/redirects`) take precedence over the global ones.

Behind a reverse proxy, `X-Forwarded-For` / `X-Forwarded-Proto` are honored.

## REST API

Authenticate with `X-Api-Key: <key>` (or `Authorization: Bearer <key>`).
Keys are created in the dashboard (*API keys*) or via the API itself, and are
shown exactly once. Roles:

- **admin** — full access.
- **author** — sees and manages only the short URLs created with that key.
- **domain** — restricted to one domain.

Errors are `application/problem+json` (RFC 7807). List endpoints support
`page` and `itemsPerPage` and return a `pagination` envelope.

### Short URLs

| Method & path | Notes |
|---|---|
| `GET /rest/v1/short-urls` | `searchTerm`, `tags`, `tagsMode=any\|all`, `group` (`group=` alone filters to ungrouped), `startDate`, `endDate`, `domain`, `orderBy=dateCreated\|shortCode\|longUrl\|title\|visits` + `-ASC/-DESC`, `excludeMaxVisitsReached`, `excludePastValidUntil` |
| `POST /rest/v1/short-urls` | body: `longUrl` (required), `customSlug`, `shortCodeLength`, `domain`, `title`, `tags`, `group`, `maxVisits`, `validSince`, `validUntil`, `forwardQuery`, `crawlable`, `redirectStatus`, `findIfExists` |
| `GET /rest/v1/short-urls/{code}` | optional `?domain=` on all `{code}` routes |
| `PATCH /rest/v1/short-urls/{code}` | partial update; send `null` to clear `title`, `group`, `maxVisits`, `validSince`, `validUntil` |
| `DELETE /rest/v1/short-urls/{code}` | |
| `GET/POST /rest/v1/short-urls/{code}/redirect-rules` | POST replaces all rules; conditions: `device`, `language`, `query-param`, `ip-address` |
| `GET /rest/v1/short-urls/{code}/visits` | `startDate`, `endDate`, `excludeBots` |
| `DELETE /rest/v1/short-urls/{code}/visits` | |

Example:

```sh
curl -H "X-Api-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"longUrl":"https://example.com/landing","customSlug":"promo","tags":["marketing"]}' \
  http://localhost:8080/rest/v1/short-urls
```

### Tags, domains, visits, stats

| Method & path | Notes |
|---|---|
| `GET /rest/v1/tags` | `withStats=true`, `searchTerm` |
| `PUT /rest/v1/tags` | `{"oldName":"a","newName":"b"}` |
| `DELETE /rest/v1/tags?tags=a,b` | |
| `GET /rest/v1/tags/{tag}/visits` | |
| `GET /rest/v1/domains` | |
| `POST /rest/v1/domains` | admin; `{"domain":"links.example.com"}` |
| `PATCH /rest/v1/domains/redirects` | admin; per-domain not-found redirects |
| `DELETE /rest/v1/domains/{authority}` | admin; default domain is protected |
| `GET /rest/v1/domains/{authority}/visits` | |
| `GET /rest/v1/visits` | admin; global counters |
| `GET /rest/v1/visits/non-orphan` · `GET/DELETE /rest/v1/visits/orphan` | admin |
| `GET /rest/v1/stats/visits-per-day` | scope with `shortCode`, `tag`, `domain` or `orphan=true`; `startDate`/`endDate` |
| `GET /rest/v1/stats/breakdown?by=country\|city\|browser\|os\|referer\|device` | same scoping |

### API keys & webhooks (admin)

| Method & path | Notes |
|---|---|
| `GET/POST /rest/v1/api-keys` | create returns `apiKey` once; body: `name`, `role`, `domain`, `expiresAt` |
| `PATCH /rest/v1/api-keys/{id}` | `{"enabled":false}` |
| `DELETE /rest/v1/api-keys/{id}` | |
| `GET/POST /rest/v1/webhooks` | create returns the signing `secret` once |
| `PATCH /rest/v1/webhooks/{id}` · `DELETE /rest/v1/webhooks/{id}` | |

Webhook deliveries are JSON:
`{"event":"visit.recorded","occurredAt":"…","data":{…}}` with an
`X-Gort-Event` header and an `X-Gort-Signature: sha256=<hex>` header —
the HMAC-SHA256 of the raw body with the webhook secret. Failed deliveries are
retried with exponential backoff (up to 6 attempts) and survive restarts.

### Misc

- `GET /rest/health` — unauthenticated health check.
- `GET /{code}/qr-code?size=300&format=png|svg&margin=1&errorCorrection=L|M|Q|H`
- `GET /robots.txt`

## Single sign-on & link groups (Keycloak / OIDC)

Setting `GORT_OIDC_ISSUER` + `GORT_OIDC_CLIENT_ID` (+ secret) adds a
"Continue with SSO" button to `/admin/login`. The flow is standard
authorization-code with PKCE and nonce; any compliant IdP works.

**Semantics**

- Users are auto-provisioned on first SSO login (matched by the stable OIDC
  `sub`; no password login for these accounts). Local accounts keep working
  unless `GORT_OIDC_ONLY=true` — keeping the initial local admin as a
  break-glass account is recommended.
- The token's groups claim (default claim name `groups`) becomes the user's
  **link groups**. Group names are normalized: Keycloak's `/marketing` and
  plain `marketing` are the same group.
- Every short URL can carry one group (`group` in the API, a picker in the
  dashboard). **Non-admin users see and manage only ungrouped links plus
  links in their own groups**, and can only assign groups they belong to.
  Admins see everything and can assign any group. This applies to the
  dashboard; API keys keep their own scoping model (admin/author/domain).
- Members of `GORT_OIDC_ADMIN_GROUP` (default `gort-admins`) get the
  dashboard admin role; everyone else signs in as a regular user.

**Keycloak setup**

1. Create a confidential client (e.g. `gort-dashboard`) with *Standard flow*
   enabled and valid redirect URI `https://your-gort/admin/oidc/callback`.
2. Add a *Group Membership* mapper to the client (or a client scope it uses)
   with token claim name `groups`, added to the ID token. "Full group path"
   on or off both work — paths are normalized.
3. Create a `gort-admins` group (or set `GORT_OIDC_ADMIN_GROUP`) and add your
   admins.
4. Configure Gort:

```sh
GORT_OIDC_ISSUER=https://keycloak.example.com/realms/main
GORT_OIDC_CLIENT_ID=gort-dashboard
GORT_OIDC_CLIENT_SECRET=…
```

Groups from Keycloak now double as link groupings: a link created in group
`marketing` is visible only to members of `/marketing` (and admins).

## Architecture

```
cmd/gort/           entry point (plus a -healthcheck probe for containers)
internal/
  core/             pure domain: constrained types (LongUrl, ShortCode,
                    TagName, DomainAuthority, typed ids), the ShortUrlSpec /
                    ShortUrlEdit constructors, Lifetime invariants,
                    redirect rule engine, IP anonymization
  data/             database/sql repositories with dialect-aware SQL
                    (SQLite + PostgreSQL), forward-only migrations,
                    transactional writes for multi-step operations
  h/                tiny programmatic HTML builder (no templates)
  web/              net/http app: redirect hot path, REST API, htmx
                    dashboard, typed domain events, background workers
                    (event fan-out, geolocation, GeoLite2 refresh, title
                    resolution, webhook delivery)
e2e/                Playwright browser tests driving the dashboard
```

The design follows the functional-core / imperative-shell style ported from
the original F# codebase:

- **Parse, don't validate.** Raw input (JSON bodies, form fields, env vars)
  is parsed once into constrained types — `LongUrl`, `ShortCode`, `TagName`,
  `DomainAuthority` — whose constructors are the only way to build them, so
  an unvalidated value cannot reach a repository.
- **One home per invariant.** `NewShortUrlSpec` / `NewShortUrlEdit` enforce
  every creation/edit rule (`maxVisits > 0`, `validSince < validUntil`, valid
  status codes, tag rules); the REST API and the dashboard both go through
  them, so the entry points cannot drift.
- **Typed everything at boundaries.** `ShortUrlID`/`DomainID`/… prevent id
  transposition; API-key roles parse fail-closed (an unknown stored role is
  an invalid key, never a default admin).
- **Errors as values.** Domain failures are typed (`ShortUrlError` and
  friends); the persistence edge translates driver errors immediately (e.g.
  duplicate key → slug-in-use conflict).
- **Atomic writes.** A short URL and its tag links are inserted in one
  transaction; rules and tag replacements likewise.
- **Events off the hot path.** The redirect path does one indexed lookup,
  evaluates rules in memory, records the raw visit, and answers; typed
  `DomainEvent`s go onto a channel, and goroutine workers handle geolocation,
  webhook fan-out and delivery.

Notes:

- Sessions use SameSite=Lax HMAC-signed cookies (signing key persisted under
  the data dir); dashboard mutations are POST-only, which blocks cross-site
  request forgery for the session cookie.
- Passwords are bcrypt-hashed; API keys and webhook secrets are stored hashed
  or server-side only and shown exactly once.
- All timestamps are UTC.

## Porting notes (Shortlink → Gort)

Everything is a 1:1 port except naming:

| Shortlink | Gort |
|---|---|
| `SHORTLINK_*` env vars | `GORT_*` |
| `sl_` API key prefix | `gort_` |
| `X-Shortlink-Event` / `X-Shortlink-Signature` webhook headers | `X-Gort-Event` / `X-Gort-Signature` |
| `shortlink_session` cookie | `gort_session` |
| "Shortlink" branding in the dashboard | "Gort" |
