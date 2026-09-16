#!/usr/bin/env bash
# Installs Python 3.12 dependencies, starts PostgreSQL and imports the Keycloak test realm.
set -euo pipefail
cd "$(dirname "$0")/.."
uv sync --project .. --python 3.12 --locked
docker compose up -d
for ((attempt=0; attempt<90; attempt++)); do
  if docker compose exec -T postgres pg_isready -U gort >/dev/null 2>&1 &&
    curl -fsS http://localhost:8081/realms/gort/.well-known/openid-configuration >/dev/null 2>&1; then
    echo "Keycloak is ready at http://localhost:8081; PostgreSQL is at localhost:15432."
    echo "Start the Python app with the isolated-schema command in e2e/keycloak-smoke/README.md."
    exit 0
  fi
  sleep 2
done
echo "The test stack did not become ready. Inspect: docker compose -f e2e/compose.yaml logs" >&2
exit 1
