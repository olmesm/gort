#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"
exec uv run --frozen ruff format --check goto tests alembic design e2e/run_server.py "$@"
