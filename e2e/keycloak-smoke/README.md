# PostgreSQL and Keycloak smoke tests

The local Compose project starts PostgreSQL 16 on `localhost:15432` and Keycloak
26.7.3 on `localhost:8081`. Both ports bind only to loopback. The imported `gort`
realm includes the dashboard client and two test users:

- `alice` / `alice-pass-123`: `gort-admins` and `team-a`.
- `bob` / `bob-pass-123`: `team-a` only.

These credentials and the imported client secret are for this local test stack.
Requires Docker, Go, Node, and `npm ci` in `e2e/`.

From the repository root:

```sh
./e2e/keycloak-smoke/setup.sh

# Each Go test uses its own schema and removes it on completion.
GORT_TEST_POSTGRES_DSN='postgres://gort:gort-local-test@localhost:15432/gort?sslmode=disable' \
  go test ./...
```

Start Gort with fresh data and a fresh PostgreSQL schema for each browser run.
This keeps the smoke test's link counts deterministic without touching other data:

```sh
SMOKE_SCHEMA="smoke_$(date +%s)"
docker compose -f e2e/compose.yaml exec -T postgres \
  psql -U gort -d gort -c "CREATE SCHEMA $SMOKE_SCHEMA"

GORT_DATA_DIR=$(mktemp -d) GORT_PORT=18300 GORT_DEFAULT_DOMAIN=localhost:18300 \
GORT_DB_DRIVER=postgres \
GORT_DB_CONNECTION="postgres://gort:gort-local-test@localhost:15432/gort?sslmode=disable&search_path=$SMOKE_SCHEMA" \
GORT_AUTO_RESOLVE_TITLES=false GORT_INITIAL_ADMIN_PASSWORD=local-admin-123 \
GORT_OIDC_ISSUER=http://localhost:8081/realms/gort \
GORT_OIDC_CLIENT_ID=gort-dashboard GORT_OIDC_CLIENT_SECRET=gort-secret \
GORT_OIDC_PROVIDER_NAME=Keycloak go run ./cmd/gort
```

In another terminal:

```sh
# Set PLAYWRIGHT_CHROMIUM_PATH if using an installed Chrome/Chromium binary.
node e2e/keycloak-smoke/test.js
```

The browser follows discovery, PKCE, code exchange and token verification against
real Keycloak. It checks provisioning, admin access, group normalization, link
visibility, unauthorized edit rejection, and scoped link creation. Regular users
land on their links page; global tags, analytics and orphan visits require admin.

Stop the local containers when finished:

```sh
docker compose -f e2e/compose.yaml down -v
```
