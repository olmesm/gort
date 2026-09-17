#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

pids=()
stop_checks() {
  for pid in "${pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
}
trap 'stop_checks; exit 130' INT
trap 'stop_checks; exit 143' TERM

./scripts/check-lint.sh &
pids+=("$!")
./scripts/check-format.sh &
pids+=("$!")
./scripts/test.sh "$@" &
pids+=("$!")

status=0
for pid in "${pids[@]}"; do
  if ! wait "$pid"; then
    status=1
  fi
done
exit "$status"
