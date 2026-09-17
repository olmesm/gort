#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

uv sync --frozen
docker compose -f e2e/compose.yaml up -d
for ((attempt=0; attempt<90; attempt++)); do
  if docker compose -f e2e/compose.yaml exec -T postgres pg_isready -U goto >/dev/null 2>&1 &&
    curl -fsS http://localhost:8081/realms/goto/.well-known/openid-configuration >/dev/null 2>&1; then
    echo 'Keycloak is ready at http://localhost:8081; PostgreSQL is at localhost:15432.'
    echo 'Start Goto with the isolated-schema command in e2e/keycloak-smoke/README.md.'
    exit 0
  fi
  sleep 2
done
echo 'The test stack did not become ready. Inspect: docker compose -f e2e/compose.yaml logs' >&2
exit 1
