# Keycloak smoke test

Verifies gort's OIDC SSO and group-scoped authorization against a **real
Keycloak** container — a real browser goes through the actual Keycloak login
form, so discovery, PKCE, the code exchange, ID-token verification and the
group-membership mapper are all exercised for real (the Go integration tests
cover the same flow against an in-process fake IdP).

Requires Docker, Go, Node and the Playwright install from `../` (`npm install`
in `e2e/`). Fresh databases only — use a throw-away `GORT_DATA_DIR`.

```sh
# 1. Boot and configure Keycloak (realm gort, client gort-dashboard,
#    groups gort-admins/team-a/team-b, users alice + bob):
./setup.sh

# 2. Start gort against it (fresh data dir):
cd ../..
GORT_DATA_DIR=$(mktemp -d) GORT_PORT=18300 \
GORT_AUTO_RESOLVE_TITLES=false \
GORT_INITIAL_ADMIN_PASSWORD=local-admin-123 \
GORT_OIDC_ISSUER=http://localhost:8081/realms/gort \
GORT_OIDC_CLIENT_ID=gort-dashboard \
GORT_OIDC_CLIENT_SECRET=gort-secret \
GORT_OIDC_PROVIDER_NAME=Keycloak \
go run ./cmd/gort &

# 3. Run the checks (set PLAYWRIGHT_CHROMIUM_PATH to reuse a local browser):
cd e2e/keycloak-smoke
node test.js

# 4. Teardown
docker rm -f gort-kc-smoke
```

What it asserts:

- alice (member of `gort-admins` + `team-a`) signs in through Keycloak, is
  auto-provisioned, and gets the dashboard **admin** role; she creates an
  ungrouped link plus one in `team-a` and one in `team-b`.
- bob (member of `team-a` only) signs in as a **regular** user: no admin
  navigation, 403 on `/admin/users`, sees only the ungrouped and `team-a`
  links, gets a 404 on the `team-b` link's edit page, and his group picker
  offers exactly `[No group, team-a]`.
- Keycloak's "full group path" values (`/team-a`) are normalized to `team-a`.
