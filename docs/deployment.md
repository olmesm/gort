# Deployment

[Back to the README](../README.md) · [Configuration](configuration.md)

Goto requires Python 3.12 and PostgreSQL. Install and activate mise in your
shell so its tools are on `PATH`. From the repository root, run `mise install`
to install the Python and uv versions configured in `.mise.toml`, then
`uv sync --frozen` to install the pinned dependencies. Set `GOTO_DB_CONNECTION`
to a PostgreSQL URL or libpq connection string, then start the application with
`uv run goto`. See the [quick start](../README.md#python-312) for local setup
and commands to use without shell activation.

## Database

Create a database owned by the application role. Startup applies Alembic
migrations before accepting requests. The role needs permission to create and
alter the application tables. Connections use UTC and the database stores
timestamps with time zones.

The migration history starts with `0001_initial`, a complete application
schema. Use an empty database for a new deployment. Unrecognized revision
histories fail startup rather than being silently stamped or modified.

The application and Alembic CLI read the same process environment. `PGOPTIONS`
is preserved unless the connection string supplies its own `options`; each
connection uses UTC. For example:

```sh
export GOTO_DB_CONNECTION='postgresql://localhost/goto'
uv run alembic upgrade head
uv run alembic check
```

For schema changes, create and review a migration before applying it:

```sh
uv run alembic revision --autogenerate -m "Describe the change"
uv run alembic upgrade head
```

Back up PostgreSQL and the signing key at `GOTO_DATA_DIR/keys/session.key`.
The application creates new key directories with mode `0700` and keys with
mode `0600`. The Docker container runs as UID/GID 65532.

## Processes and background work

The CLI starts one Uvicorn process with background threads for page-title
lookup and webhook delivery. `GOTO_WORKERS_ENABLED=false` disables those
threads. Use `uv run goto` so the server lets Goto validate forwarded headers
against `GOTO_TRUSTED_PROXIES`.

Webhook deliveries persist across restarts. A conditional database update
claims each due delivery for five minutes. Failed requests retry with
exponential backoff, up to six attempts. Receivers should tolerate duplicate
events if a process stops between sending a request and saving its result.
Deliveries that exhaust all attempts remain marked failed; there is no
automatic replay.

Rate limits are per process. When deploying multiple web processes, designate
one for background work and enforce shared rate limits at the reverse proxy.
All processes must use the same PostgreSQL database and session-signing key.
Apply migrations before starting additional web processes; startup migrations
do not have a lock shared between processes.

Outbound title and webhook requests validate DNS answers and connect to the
validated address while preserving TLS hostname verification. Every redirect
receives the same checks. Proxy environment variables cannot bypass them.

## Testing

After installing the Python dependencies, set `GOTO_TEST_POSTGRES_DSN` to a
disposable database. `./scripts/check.sh` runs Ruff and pytest; database tests
create and drop isolated schemas. The browser suite also needs Node.js 22 and
npm, and uses the same database setting with an isolated schema for each run:

```sh
export GOTO_TEST_POSTGRES_DSN='postgresql://localhost/goto_test'
createdb goto_test
./scripts/check.sh
npm --prefix e2e ci
./e2e/node_modules/.bin/playwright install chromium
./scripts/test-browser.sh
```

The test role must be able to create schemas and drop the schemas it owns.
CI supplies PostgreSQL to the Python and browser jobs. See the
[live Keycloak test stack](../e2e/keycloak-smoke/README.md) for identity-provider
login and group-access checks.
