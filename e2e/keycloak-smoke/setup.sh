#!/usr/bin/env bash
# Starts isolated local PostgreSQL and imports the Keycloak test realm.
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose up -d
for ((attempt=0; attempt<90; attempt++)); do
  if curl -fsS http://localhost:8081/realms/gort/.well-known/openid-configuration >/dev/null 2>&1; then
    echo "Keycloak is ready at http://localhost:8081; PostgreSQL is at localhost:15432."
    exit 0
  fi
  sleep 2
done
echo "Keycloak did not become ready. Inspect: docker compose -f e2e/compose.yaml logs keycloak" >&2
exit 1
