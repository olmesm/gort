#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"
uv run --frozen ruff check --fix goto tests alembic design e2e/run_server.py "$@"
exec uv run --frozen ruff format goto tests alembic design e2e/run_server.py "$@"
