# Gort

A self-hosted URL shortener written in Go. One binary with a server-rendered
admin dashboard, visit analytics, REST with OpenAPI, and GraphQL. Built with
[Chi](https://github.com/go-chi/chi), [Huma](https://huma.rocks/),
[gqlgen](https://gqlgen.com/) and [htmx](https://htmx.org/).

Gort started as a Go port of [Shortlink](https://github.com/olmesm/shortlink)
(F#). It retains the v1 REST routes and adds a navy dashboard, generated API
documentation and GraphQL.

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
  pagination, search, ordering and rate limiting. Huma generates an OpenAPI
  3.1 contract and interactive REST documentation from the registered operations.
- **GraphQL**: queries and mutations at `/graphql`, with the same API keys,
  permissions and operations as REST. Fetch links with nested visits and redirect
  rules in a single request. Includes a local request editor and schema download.
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

### Prebuilt binary (no dependencies, no config)

Gort is a single static binary — no libsqlite3, no libpq, nothing to
install. Download the archive for your platform from the
[releases page](https://github.com/olmesm/gort/releases) and run it:

```sh
# pick one: linux_amd64, linux_arm64, darwin_amd64, darwin_arm64
VERSION=0.2.1
curl -sSL "https://github.com/olmesm/gort/releases/download/v${VERSION}/gort_${VERSION}_linux_amd64.tar.gz" | tar xz
./gort
```

That's it. With no environment variables set, Gort:

- listens on <http://localhost:8080>;
- creates `./data/` next to you, holding the SQLite database
  (`data/gort.db`) and the session-signing key;
- creates an `admin` user and **prints its generated password in the log**:

  ```
  level=WARN msg="Created initial admin user 'admin' with generated password: I1OVTMBoW8NAfXMF — log in at /admin/login and change it."
  level=INFO msg="Gort listening on http://0.0.0.0:8080"
  ```

Set `GORT_INITIAL_ADMIN_PASSWORD=…` before the first start if you'd rather
choose it. `checksums.txt` on each release carries SHA-256 sums of the
archives. (macOS: if you downloaded through a browser rather than `curl`,
Gatekeeper may quarantine the binary — `xattr -d com.apple.quarantine gort`
clears it.)

On Unix, Gort creates new data directories with mode `0700` and new SQLite
files with mode `0600`. Existing directories and files keep their permissions.
For an existing installation, restrict the data directory and database to the
service account, including any SQLite WAL files and backups. The database can
contain visit data and webhook signing secrets.

#### Try it out manually

1. Open <http://localhost:8080/admin> and log in as `admin` with the printed
   password.
2. *Short URLs → New*: paste any long URL, optionally a custom slug such as
   `godoc`, and save.
3. Visit <http://localhost:8080/godoc> — you're redirected, and the visit
   shows up under the link's *Analytics*.
4. <http://localhost:8080/godoc/qr-code> gives you a PNG QR code.
5. *API keys → Create API key* (role *admin*) shows a `gort_…` key once; use it against
   the REST API:

   ```sh
   KEY=gort_...
   curl -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' \
        -d '{"longUrl":"https://example.com","tags":["demo"]}' \
        http://localhost:8080/rest/v1/short-urls

   curl -H "X-Api-Key: $KEY" http://localhost:8080/rest/v1/short-urls
   curl -H "X-Api-Key: $KEY" http://localhost:8080/rest/v1/short-urls/godoc/visits
   ```

Stop it with Ctrl-C; the state lives entirely in `./data/`, so `rm -rf data`
resets everything. To serve real short links, set `GORT_DEFAULT_DOMAIN` to
the public hostname (e.g. `go.example.com`) and `GORT_USE_HTTPS=true` behind
your TLS-terminating proxy.

The same binary talks to PostgreSQL when told to:

```sh
GORT_DB_DRIVER=postgres GORT_DB_CONNECTION="postgres://gort:gort@localhost/gort" ./gort
```

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

Requires Go 1.26+.

```sh
go run ./cmd/gort
```

Run the test suite (unit + full-stack integration tests):

```sh
go test ./...
```

Run the browser end-to-end tests (Playwright driving the real dashboard in
Chromium, covering login, short URL lifecycle, htmx live search,
redirect rules, analytics, tags, domains, API keys, webhooks, users, link groups and
orphan visits, responsive layout and both API documentation clients; requires Node.js and Go):

```sh
cd e2e
npm install
npx playwright install chromium   # once; or set PLAYWRIGHT_CHROMIUM_PATH to an existing binary
npm test
```

The e2e config builds and starts the app itself on port 18100 with a
throw-away SQLite database, so no setup is needed.

## Configuration

Set `GORT_USE_HTTPS=true` behind HTTPS termination so session cookies are marked
Secure, and configure only the actual proxy CIDRs in `GORT_TRUSTED_PROXIES`.

For real PostgreSQL and Keycloak tests, see [the local integration stack](e2e/keycloak-smoke/README.md).

Everything is configured through environment variables.

| Variable | Default | Purpose |
|---|---|---|
| `GORT_PORT` | `8080` | HTTP listen port |
| `GORT_DEFAULT_DOMAIN` | `localhost:<port>` | Authority used to build short URLs when no domain is given |
| `GORT_USE_HTTPS` | `false` | Render HTTPS short URLs and set Secure session cookies |
| `GORT_DATA_DIR` | `./data` | SQLite db, GeoLite2 db, session signing keys |
| `GORT_DB_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `GORT_DB_CONNECTION` | SQLite in data dir | SQLite file path or PostgreSQL connection string |
| `GORT_SHORT_CODE_LENGTH` | `5` | Length of generated codes (min 4) |
| `GORT_REDIRECT_STATUS` | `302` | Default redirect status (301/302/307/308) |
| `GORT_WEBHOOKS_ENABLED` | `false` | Enable webhook management, event fan-out and delivery workers |
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
| `GORT_TRUSTED_PROXIES` | *(unset)* | Comma-separated proxy CIDRs allowed to supply forwarded client IP and scheme. Headers from other peers are ignored. |
| `GORT_ALLOW_PRIVATE_OUTBOUND` | `false` | Allow title fetching and webhooks to private/loopback destinations. Enable only for trusted deployments that need internal targets. |
| `GORT_RATE_LIMIT_PER_MINUTE` | `120` | Mutating REST calls, GraphQL POSTs and password login attempts per minute per IP (0 disables) |
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

Open `/rest/docs` for the interactive reference or download `/rest/openapi.json`
(or `/rest/openapi.yaml`). The contract is generated from all registered REST
operations, including health. Webhook operations appear only when enabled.
The API keys page links to the docs and includes a curl example.


Authenticate with `X-Api-Key: <key>` (or `Authorization: Bearer <key>`).
Keys are created in the dashboard (*API keys*) or via the API itself, and are
shown exactly once. Roles:

- **admin** — full access.
- **author** — sees and manages only the short URLs created with that key.
- **domain** — restricted to one domain.

Only admin keys can rename or delete shared tags, read tag statistics or visits,
or view global/orphan statistics. Domain keys can view statistics for their own
domain; author keys must select an owned short code. Tag names and registered
domain names remain visible to authenticated API keys.

Errors are `application/problem+json` (RFC 7807). Short URL, tag and visit lists
support `page` and `itemsPerPage` and return a `pagination` envelope. Domain,
API-key and webhook REST lists retain their v1 unpaginated `data` envelope.
Dashboard lists have separate pagination and filters.

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
| `GET /rest/v1/tags` | `withStats=true` (admin), `searchTerm` |
| `PUT /rest/v1/tags` | admin; `{"oldName":"a","newName":"b"}` |
| `DELETE /rest/v1/tags?tags=a,b` | admin |
| `GET /rest/v1/tags/{tag}/visits` | admin |
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

Webhooks are disabled by default. Set `GORT_WEBHOOKS_ENABLED=true` and restart
to enable the dashboard page, REST endpoints, GraphQL operations and delivery workers. While disabled,
no new events are queued or delivered; stored webhook configurations and pending
deliveries are retained. Pending deliveries resume when webhooks are enabled again.
Events that occur while disabled are not replayed.

Webhook deliveries are JSON:
`{"event":"visit.recorded","occurredAt":"…","data":{…}}` with an
`X-Gort-Event` header and an `X-Gort-Signature: sha256=<hex>` header —
the HMAC-SHA256 of the raw body with the webhook secret. Failed deliveries are
retried with exponential backoff (up to 6 attempts) and survive restarts.

### Misc

- `GET /rest/health` — unauthenticated health check.
- `GET /{code}/qr-code?size=300&format=png|svg&margin=1&errorCorrection=L|M|Q|H`
- `GET /robots.txt`

## GraphQL

Open `/graphql/docs` for examples and a request editor. Download the schema at
`/graphql/schema.graphql`, or use authenticated introspection from your own
GraphQL client. Documentation and schemas are public; API operations require a
key. Documentation assets are bundled, and the built-in editors do not persist
credentials or send requests through an external proxy.

```sh
curl http://localhost:8080/graphql \
  -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"query":"query Links($count: Int!) { shortURLs(filter: {itemsPerPage: $count}) { data { shortCode shortUrl visitsSummary { total } visits(filter: {itemsPerPage: 2}) { data { date browser } } } pagination { totalItems } } }","variables":{"count":5}}'
```

Queries cover links, tags, domains, visits, statistics, API keys and webhooks.
Mutations create, update and delete those resources, replace redirect rules and
clear visits. `updateShortURL` preserves omitted fields and clears nullable
fields when passed explicit `null`. Empty `group: ""` selects ungrouped links;
omitting it selects all accessible groups.

Queries accept GET or POST; mutations require POST. There are no subscriptions.
Requests are limited to 1 MiB, 10,000 parser tokens and 10,000 complexity points.
Page sizes multiply query cost, including nested visit pages. POST requests count
against `GORT_RATE_LIMIT_PER_MINUTE`, including queries. Gqlgen limits concurrent
field resolution to 16 workers per request.

Check `errors` even when HTTP status is 200. Resolver errors include
`extensions.code` and `extensions.status`; authentication failures return HTTP
401 with a problem document. Schema validation and complexity errors use gqlgen's
GraphQL error format. API keys and webhook secrets are shown once on creation;
selecting those fields in list queries returns an empty string. Disabled webhook
operations return a GraphQL error with status 404.

The `/graphql` path and its children are reserved. If upgrading an instance with
links using that prefix, recreate those links under another slug before upgrading.
No database migration is needed for this release.

### Regenerate the GraphQL server

Edit `internal/web/schema.graphqls` and its bindings in `gqlgen.yml`, then run:

```sh
go generate ./internal/web
go test ./...
```

The generator is pinned in `go.mod`. Commit the generated server and model files.
Resolvers in `gql_resolvers.go` are maintained adapters to the shared operations;
generation preserves them, and new fields require corresponding resolvers.
There is no generation step when building a release from the checkout.

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

To verify the whole flow against a real Keycloak container (real browser
through the actual Keycloak login form), see
[`e2e/keycloak-smoke/`](e2e/keycloak-smoke/README.md).

## Architecture

```
cmd/gort/           entry point (plus a -healthcheck probe for containers)
internal/
  core/             pure domain: constrained types (LongURL, ShortCode,
                    TagName, DomainAuthority, typed ids), the ShortURLSpec /
                    ShortUrlEdit constructors, Lifetime invariants,
                    redirect rule engine, IP anonymization
  data/             database/sql repositories with dialect-aware SQL
                    (SQLite + PostgreSQL), forward-only migrations,
                    transactional writes for multi-step operations
  web/              Chi routing, Huma REST/OpenAPI, gqlgen GraphQL, htmx
                    dashboard, typed domain events, background workers
                    (event fan-out, geolocation, GeoLite2 refresh, title
                    resolution, webhook delivery)
e2e/                Playwright browser tests driving the dashboard
```

The design follows the functional-core / imperative-shell style ported from
the original F# codebase:

- **Parse, don't validate.** Raw input (JSON bodies, form fields, env vars)
  is parsed once into constrained types — `LongURL`, `ShortCode`, `TagName`,
  `DomainAuthority` — whose constructors are the only way to build them, so
  an unvalidated value cannot reach a repository.
- **One home per invariant.** `NewShortURLSpec` / `NewShortURLEdit` enforce
  every creation/edit rule (`maxVisits > 0`, `validSince < validUntil`, valid
  status codes, tag rules); REST, GraphQL and the dashboard go through them.
- **Typed everything at boundaries.** `ShortURLID`/`DomainID`/… prevent id
  transposition; API-key roles parse fail-closed (an unknown stored role is
  an invalid key, never a default admin).
- **Errors as values.** Domain failures are sentinel error categories
  (`core.ErrSlugInUse` and friends, matched with `errors.Is`); the
  persistence edge translates driver errors immediately (e.g. duplicate key
  → slug-in-use conflict).
- **Shared API operations.** Huma binds REST inputs and generates OpenAPI from
  typed operation signatures. GraphQL resolvers call those same operations, which
  enforce permissions and return DTOs or errors. REST preserves v1 envelopes and
  problem documents; GraphQL presents the results in its own response format.
  Dashboard and redirect handlers use the existing `func(w, r) error` adapter.
  Unexpected errors are logged and masked at the transport boundary.
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
