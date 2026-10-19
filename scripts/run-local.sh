#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
if [[ -f .env ]]; then
  set -a
  source .env
  set +a
fi
export PORT="${PORT:-8080}"
export DATA_DIR="${DATA_DIR:-data}"
printf 'Website: http://localhost:%s/fi/\nAdmin: http://localhost:%s/admin\n' "$PORT" "$PORT"
exec go run ./cmd/server
