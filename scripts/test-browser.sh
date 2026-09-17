#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root/e2e"
if [[ ! -f node_modules/@playwright/test/cli.js ]]; then
  echo 'Install browser dependencies with: npm ci --prefix e2e' >&2
  exit 1
fi
exec node node_modules/@playwright/test/cli.js test "$@"
