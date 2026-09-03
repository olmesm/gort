#!/usr/bin/env bash
# Boots a throw-away Keycloak container and configures a realm for the
# smoke test: confidential client with a group-membership mapper, groups
# gort-admins / team-a / team-b, and two users:
#   alice (gort-admins + team-a)  -> gort admin
#   bob   (team-a)                -> regular user, team-a scope
set -euo pipefail

KC_PORT="${KC_PORT:-8081}"
GORT_PORT="${GORT_PORT:-18300}"
KC_IMAGE="${KC_IMAGE:-quay.io/keycloak/keycloak:26.0}"
CONTAINER="${KC_CONTAINER:-gort-kc-smoke}"

docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$CONTAINER" -p "$KC_PORT:8080" \
  -e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin \
  "$KC_IMAGE" start-dev >/dev/null
# When the docker daemon has no bridge network (some sandboxes), fall back
# to host networking on the same port.
if ! docker ps --format '{{.Names}}' | grep -q "^$CONTAINER$"; then
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$CONTAINER" --network=host \
    -e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin \
    "$KC_IMAGE" start-dev --http-port="$KC_PORT" >/dev/null
fi

echo "Waiting for Keycloak on :$KC_PORT …"
for _ in $(seq 1 90); do
  if curl -fs "http://localhost:$KC_PORT/realms/master/.well-known/openid-configuration" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

KC="docker exec $CONTAINER /opt/keycloak/bin/kcadm.sh"
$KC config credentials --server "http://localhost:$KC_PORT" --realm master --user admin --password admin

$KC create realms -s realm=gort -s enabled=true
$KC create clients -r gort -s clientId=gort-dashboard -s enabled=true -s publicClient=false \
  -s secret=gort-secret -s standardFlowEnabled=true -s directAccessGrantsEnabled=false \
  -s "redirectUris=[\"http://localhost:$GORT_PORT/admin/oidc/callback\"]"

CID=$($KC get clients -r gort -q clientId=gort-dashboard --fields id --format csv --noquotes)
$KC create "clients/$CID/protocol-mappers/models" -r gort \
  -s name=groups -s protocol=openid-connect -s protocolMapper=oidc-group-membership-mapper \
  -s 'config."claim.name"=groups' -s 'config."full.path"=true' \
  -s 'config."id.token.claim"=true' -s 'config."access.token.claim"=true' \
  -s 'config."userinfo.token.claim"=true'

for g in gort-admins team-a team-b; do $KC create groups -r gort -s name="$g"; done

for u in alice bob; do
  $KC create users -r gort -s username="$u" -s enabled=true \
    -s email="$u@example.test" -s emailVerified=true -s firstName="$u" -s lastName=Test
  $KC set-password -r gort --username "$u" --new-password "$u-pass-123"
done

uid() { $KC get users -r gort -q username="$1" --fields id --format csv --noquotes; }
gid() { $KC get groups -r gort --fields id,name --format csv --noquotes | grep ",$1$" | cut -d, -f1; }

$KC update "users/$(uid alice)/groups/$(gid gort-admins)" -r gort -n
$KC update "users/$(uid alice)/groups/$(gid team-a)" -r gort -n
$KC update "users/$(uid bob)/groups/$(gid team-a)" -r gort -n

cat <<MSG

Keycloak ready. Start gort with:

  GORT_PORT=$GORT_PORT \\
  GORT_OIDC_ISSUER=http://localhost:$KC_PORT/realms/gort \\
  GORT_OIDC_CLIENT_ID=gort-dashboard \\
  GORT_OIDC_CLIENT_SECRET=gort-secret \\
  GORT_OIDC_PROVIDER_NAME=Keycloak \\
  go run ./cmd/gort

Then: node test.js   (users: alice/alice-pass-123, bob/bob-pass-123)
Teardown: docker rm -f $CONTAINER
MSG
