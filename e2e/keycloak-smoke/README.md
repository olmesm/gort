# PostgreSQL and Keycloak smoke tests

[Back to the README](../../README.md) · [Deployment](../../docs/deployment.md)

The Compose stack runs PostgreSQL 16 on `localhost:15432` and Keycloak 26.7.3 on
`localhost:8081`. Both ports bind to loopback. The imported `gort` realm contains
the dashboard client and these users:

| Username | Password | Groups |
|---|---|---|
| `alice` | `alice-pass-123` | `gort-admins`, `team-a` |
| `bob` | `bob-pass-123` | `team-a` |

The credentials and client secret are for this local test stack only.
Install Docker, uv and Node, then run `npm ci` and
`npx playwright install chromium` in `e2e/`. The setup script installs the locked
dependencies with Python 3.12.

## Start the stack and run Python tests

From the repository root:

```sh
./e2e/keycloak-smoke/setup.sh

# Every database test creates and removes its own PostgreSQL schema.
GORT_TEST_POSTGRES_DSN='postgres://gort:gort-local-test@localhost:15432/gort?sslmode=disable' \
  uv run pytest -q
```

The suite uses PostgreSQL for migrations, CRUD, permissions, analytics and
background delivery. Each database test gets an isolated schema.

## Run the browser test

Create a fresh PostgreSQL schema and data directory for each run. The test expects
an empty link list:

```sh
SMOKE_SCHEMA="smoke_$(date +%s)"
SMOKE_DATA_DIR=$(mktemp -d)
docker compose -f e2e/compose.yaml exec -T postgres \
  psql -U gort -d gort -c "CREATE SCHEMA $SMOKE_SCHEMA"

PGOPTIONS="-c search_path=$SMOKE_SCHEMA" \
GORT_DATA_DIR="$SMOKE_DATA_DIR" GORT_PORT=18300 GORT_DEFAULT_DOMAIN=localhost:18300 \
GORT_DB_CONNECTION="postgres://gort:gort-local-test@localhost:15432/gort?sslmode=disable" \
GORT_AUTO_RESOLVE_TITLES=false GORT_INITIAL_ADMIN_PASSWORD=local-admin-123 \
GORT_OIDC_ISSUER=http://localhost:8081/realms/gort \
GORT_OIDC_CLIENT_ID=gort-dashboard GORT_OIDC_CLIENT_SECRET=gort-secret \
GORT_OIDC_PROVIDER_NAME=Keycloak uv run gort
```

In another terminal, run the browser test. Set `PLAYWRIGHT_CHROMIUM_PATH` to use
an installed Chrome or Chromium binary:

```sh
node e2e/keycloak-smoke/test.js
```

The test signs in through Keycloak using discovery, PKCE, code exchange and
token verification, then checks provisioning and group access. It verifies admin
pages, link visibility, rejected edits and link creation. Regular users land on their
links page; global tags, analytics and orphan visits require admin.

The signed mock-provider OIDC tests and dashboard browser suite also run
without Keycloak. This smoke test exercises a live identity provider.

## Stop the stack

Stop Gort with Ctrl-C in its original terminal. That terminal still has the
schema and data-directory variables needed to remove this run's data:

```sh
docker compose -f e2e/compose.yaml exec -T postgres \
  psql -U gort -d gort -c "DROP SCHEMA $SMOKE_SCHEMA CASCADE"
rm -r "$SMOKE_DATA_DIR"
```

To stop the shared test stack and delete its volumes:

```sh
docker compose -f e2e/compose.yaml down -v
```
