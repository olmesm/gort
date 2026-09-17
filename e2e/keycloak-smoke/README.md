# PostgreSQL and Keycloak smoke tests

[Back to the README](../../README.md) · [Deployment](../../docs/deployment.md)

The Compose stack runs PostgreSQL 16 on `localhost:15432` and Keycloak 26.7.3 on
`localhost:8081`. Both ports bind to loopback. The imported `goto` realm contains
the dashboard client and these users:

| Username | Password | Groups |
|---|---|---|
| `alice` | `alice-pass-123` | `goto-admins`, `team-a` |
| `bob` | `bob-pass-123` | `team-a` |

The credentials and client secret are for this local test stack only.
Install Docker with Compose, mise, Node.js 22 and npm. Start Docker and activate
mise in your shell so Python and uv are on `PATH`. From the repository root,
run `mise install`, `npm --prefix e2e ci` and
`./e2e/node_modules/.bin/playwright install chromium`. The setup script installs
the locked Python dependencies.

## Start the stack and run Python tests

From the repository root:

```sh
./scripts/setup-keycloak.sh

# Every database test creates and removes its own PostgreSQL schema.
GOTO_TEST_POSTGRES_DSN='postgres://goto:goto-local-test@localhost:15432/goto?sslmode=disable' \
  ./scripts/test.sh
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
  psql -U goto -d goto -c "CREATE SCHEMA $SMOKE_SCHEMA"

PGOPTIONS="-c search_path=$SMOKE_SCHEMA" \
GOTO_DATA_DIR="$SMOKE_DATA_DIR" GOTO_PORT=18300 GOTO_DEFAULT_DOMAIN=localhost:18300 \
GOTO_DB_CONNECTION="postgres://goto:goto-local-test@localhost:15432/goto?sslmode=disable" \
GOTO_AUTO_RESOLVE_TITLES=false GOTO_INITIAL_ADMIN_PASSWORD=local-admin-123 \
GOTO_OIDC_ISSUER=http://localhost:8081/realms/goto \
GOTO_OIDC_CLIENT_ID=goto-dashboard GOTO_OIDC_CLIENT_SECRET=goto-secret \
GOTO_OIDC_PROVIDER_NAME=Keycloak uv run goto
```

In another terminal, run the browser test. Set `PLAYWRIGHT_CHROMIUM_PATH` to use
an installed Chrome or Chromium binary:

```sh
./scripts/test-keycloak.sh
```

The test signs in through Keycloak using discovery, PKCE, code exchange and
token verification, then checks provisioning and group access. It verifies admin
pages, link visibility, rejected edits and link creation. Regular users land on their
links page; global tags, analytics and orphan visits require admin.

The signed mock-provider OIDC tests and dashboard browser suite also run
without Keycloak. This smoke test exercises a live identity provider.

## Stop the stack

Stop Goto with Ctrl-C in its original terminal. That terminal still has the
schema and data-directory variables needed to remove this run's data:

```sh
docker compose -f e2e/compose.yaml exec -T postgres \
  psql -U goto -d goto -c "DROP SCHEMA $SMOKE_SCHEMA CASCADE"
rm -r "$SMOKE_DATA_DIR"
```

To stop the shared test stack and delete its volumes:

```sh
docker compose -f e2e/compose.yaml down -v
```
