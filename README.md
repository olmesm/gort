# Goto

A self-hosted URL shortener for Python 3.12, built with FastAPI, Pydantic,
SQLModel, Alembic, Jinja and htmx. Includes a server-rendered admin dashboard,
visit analytics, REST with OpenAPI, and a schema-first GraphQL API.

See [deployment](docs/deployment.md), [configuration](docs/configuration.md),
[API reference](docs/api.md) and [security controls](docs/security-review.md).

## Features

- Create short URLs with generated codes or custom slugs such as `docs/intro`.
  Add titles and tags, choose a 301/302/307/308 redirect, and control query-string
  forwarding. Goto can fetch page titles automatically.
- Serve multiple domains from one instance. Codes are unique within each domain;
  creating a link on an unregistered domain registers that domain. Each domain
  can have its own not-found redirects.
- Limit links by date or visit count with `validSince`, `validUntil` and
  `maxVisits`. Expired links use the configured not-found behavior.
- Set redirect rules by device, `Accept-Language`, query parameter or IP/CIDR.
  Rules run in priority order.
- Track visits by referrer, browser, operating system, device and bot status.
  IPs are anonymized by default; IP capture and all tracking can be disabled.
  Base URL hits, unknown codes and other 404s are tracked as orphan visits.
- Manage links through the REST API at `/rest/v1`, GraphQL at `/graphql`, or the
  dashboard at `/admin`. REST includes OpenAPI documentation and RFC 7807 errors.
  Both APIs use admin, author or domain-scoped keys.
- Use the dashboard for search, filtered lists, visit charts, QR previews and
  redirect rules. Pages use server-rendered HTML and htmx, with no JavaScript
  build step.
- Sign in through OIDC with Keycloak or another compatible provider. Goto creates
  users on first login and uses their groups to control dashboard link access.
- Generate PNG or SVG QR codes at `GET /{code}/qr-code`, with size, margin and
  error-correction options. `robots.txt` lists links marked `crawlable`.
- Enable HMAC-SHA256-signed webhooks for `url.created`, `visit.recorded` and
  `orphan_visit.recorded`. A persistent queue retries failed deliveries.
- Store data in PostgreSQL. Alembic migrations run at startup.

## Quick start

### Python 3.12

Install and activate [mise](https://mise.jdx.dev/) in your shell, then start
PostgreSQL. From the repository root, install the configured Python and uv
versions, create an empty application database and start Goto:

```sh
mise install
createdb goto
uv sync --frozen
GOTO_DB_CONNECTION='postgresql://localhost/goto' uv run goto
```

The `.mise.toml` file manages Python 3.12 and uv. `python -m goto` also works
inside the project environment. Use `GOTO_DB_CONNECTION` for a PostgreSQL URL or libpq
connection string when connecting to a different server or account.

With default settings, Goto:

- listens on <http://localhost:8080>;
- stores application records in PostgreSQL and the session-signing key in `./data/`;
- creates an `admin` user and prints the generated password in the startup log.

Example output, with the password replaced by a placeholder:

```text
WARNING goto.app Created initial admin user 'admin' with generated password: <generated-password>
INFO: Uvicorn running on http://0.0.0.0:8080
```

Set `GOTO_INITIAL_ADMIN_PASSWORD` before the first start to choose a password.

Goto creates its data directory with mode `0700` and its session-signing key
with mode `0600`. Keep the signing key with your application backups so signed
sessions survive a restore.

#### Create a link and try the API

1. Open <http://localhost:8080/admin> and log in as `admin` with the printed
   password.
2. Open *Short URLs*, choose *New short URL*, enter a long URL and the slug
   `demo`, then save.
3. Visit <http://localhost:8080/demo>. Goto redirects you and records the visit.
   Click the link's visit count in *Short URLs* to see its analytics.
4. Open <http://localhost:8080/demo/qr-code> for a PNG QR code.
5. Open *API keys*, choose *Create API key* with the *admin* role, and copy the
   key. Goto shows it only once. Use it in these REST requests:

   ```sh
   KEY=goto_...
   curl -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' \
        -d '{"longUrl":"https://example.com","tags":["demo"]}' \
        http://localhost:8080/rest/v1/short-urls

   curl -H "X-Api-Key: $KEY" http://localhost:8080/rest/v1/short-urls
   curl -H "X-Api-Key: $KEY" http://localhost:8080/rest/v1/short-urls/demo/visits
   ```

Stop Goto with Ctrl-C. Application records remain in PostgreSQL. The data
directory contains the persistent session-signing key.

For a deployment, set `GOTO_DEFAULT_DOMAIN` to its public hostname, such as
`go.example.com`. Behind a TLS proxy, set `GOTO_USE_HTTPS=true` and configure
`GOTO_TRUSTED_PROXIES` with the proxy's CIDRs.

### Docker

Compose starts the application and PostgreSQL together:

```sh
export GOTO_POSTGRES_PASSWORD="$(openssl rand -hex 32)"
docker compose up --build
```

Open <http://localhost:8080/admin>. Use the generated password in the container
log, or set `GOTO_INITIAL_ADMIN_PASSWORD` before the first `docker compose up`
to choose one.

Save `GOTO_POSTGRES_PASSWORD` in your secret manager or a local `.env` file with
mode `0600`, and reuse it for subsequent starts. Git ignores `.env` files, and
Docker excludes them from the build context. For an existing database, use its
current password. Changing this variable does not change the database password.

### Development and checks

```sh
export GOTO_TEST_POSTGRES_DSN='postgresql://localhost/goto_test'
createdb goto_test
mise install
uv sync --frozen
./scripts/check.sh
./scripts/fmt.sh
```

`check.sh` runs Ruff lint, format checks and pytest in parallel. `fmt.sh` applies
Ruff formatting. Tests cover domain rules, database migrations, REST, GraphQL,
sessions, signed OIDC login, redirects and webhook retries.
The suite requires `GOTO_TEST_POSTGRES_DSN`. Database tests create isolated
temporary PostgreSQL schemas and remove them afterward.

The Playwright suite runs the Python application:

```sh
npm --prefix e2e ci
./e2e/node_modules/.bin/playwright install chromium
./scripts/test-browser.sh
```

These browser checks also require `GOTO_TEST_POSTGRES_DSN`. Set
`PLAYWRIGHT_CHROMIUM_PATH` to use an installed Chrome or Chromium. Browser
checks cover login, link management, redirect rules, analytics, admin pages,
responsive layouts and the API editors. Browser runs use isolated PostgreSQL
schemas, temporary signing keys and browser authentication files under `e2e/.auth/`.

See [the deployment guide](docs/deployment.md) for database configuration,
migrations and background workers.

## Configuration

See the [configuration reference](docs/configuration.md) for environment
variables and defaults. Goto reads process environment variables; the application
does not automatically load `.env`.

## REST API

REST is available at `/rest/v1`, with an interactive reference at `/rest/docs`.
Use `X-Api-Key: <key>` or `Authorization: Bearer <key>`.
See the [REST reference](docs/api.md#rest-api) for operations, permissions,
pagination, webhooks and examples.

## GraphQL

GraphQL is available at `/graphql`, with a request editor at `/graphql/docs`
and a schema download at `/graphql/schema.graphql`. It uses the same keys and
permissions as REST. See the [GraphQL reference](docs/api.md#graphql) for query
examples, request limits and error handling.

## Single sign-on and link groups

Set `GOTO_OIDC_ISSUER`, `GOTO_OIDC_CLIENT_ID` and, for confidential clients,
`GOTO_OIDC_CLIENT_SECRET` to add "Continue with SSO" at `/admin/login`.
Login uses the authorization-code flow with PKCE and a nonce.

Goto matches OIDC users by their stable `sub` claim and creates an account on
first login. These accounts cannot use password login. Local accounts remain
available unless `GOTO_OIDC_ONLY=true`.

The token's `groups` claim controls dashboard link access. Goto normalizes group
paths, so `/marketing` and `marketing` name the same group. Each link can have
one group. Regular users can see and manage ungrouped links and links in their
groups; they can assign only groups they belong to. Admins can manage all links
and assign any group. Members of `GOTO_OIDC_ADMIN_GROUP`, which defaults to
`goto-admins`, receive the dashboard admin role.

API keys use their own admin, author or domain permissions; dashboard groups do
not change API-key access.

### Keycloak setup

1. Create a confidential client such as `goto-dashboard`. Enable *Standard flow*
   and set the redirect URI to `https://your-goto/admin/oidc/callback`.
2. Add a *Group Membership* mapper to the client or one of its client scopes.
   Set the token claim name to `groups` and include it in the ID token.
   Either *Full group path* setting works.
3. Create a `goto-admins` group and add your admins, or set
   `GOTO_OIDC_ADMIN_GROUP` to an existing group.
4. Configure Goto:

```sh
export GOTO_OIDC_ISSUER=https://keycloak.example.com/realms/main
export GOTO_OIDC_CLIENT_ID=goto-dashboard
export GOTO_OIDC_CLIENT_SECRET=your-client-secret
uv run goto
```

A link assigned to `marketing` is visible in the dashboard to members of that
group and to admins. See [the Keycloak smoke test](e2e/keycloak-smoke/README.md)
for a browser test of login and group access.

## Architecture

| Path | Responsibility |
|---|---|
| `goto/app.py`, `goto/config.py` | Application lifecycle, routes, configuration and first-run setup |
| `goto/domain.py` | Pydantic input validation, redirect conditions, expiry and IP anonymization |
| `goto/models.py`, `goto/db.py` | SQLModel records, PostgreSQL connections and migrations |
| `goto/services.py` | Link operations, transactions, queries and response mapping |
| `goto/api.py`, `goto/graphql.py` | REST/OpenAPI and GraphQL transports and API-key permissions |
| `goto/ui.py`, `goto/auth.py`, `goto/templates/` | Jinja/htmx dashboard, signed sessions and OIDC |
| `goto/public.py`, `goto/workers.py` | Redirects, visit capture, titles and webhooks |
| `goto/outbound.py`, `goto/middleware.py` | Outbound destination checks, trusted proxies and browser protection |
| `alembic/` | PostgreSQL schema migrations |
| `tests/`, `e2e/` | Python unit/integration tests and Playwright browser checks |

Database records are separate from API input models. Link creation and edits
validate domain rules before persistence. Creating a link and replacing its
tags is transactional. API-key permissions and dashboard group permissions
remain separate.

PostgreSQL stores timezone-aware timestamps. Each connection uses UTC.
Alembic manages the schema through explicit migrations. Startup applies pending
migrations before the application serves requests.

See [security controls and limits](docs/security-review.md) for session behavior,
API-key storage, outbound requests and test coverage.
