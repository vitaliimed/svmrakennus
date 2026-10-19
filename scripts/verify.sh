#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
if [[ -n "$(gofmt -l cmd internal web/embed.go)" ]]; then
  echo 'Run gofmt -w cmd internal web/embed.go' >&2
  exit 1
fi
go vet ./...
go test -race -count=1 ./...
go build -buildvcs=false -o /tmp/svm-verify-server ./cmd/server
for file in web/static/admin.js web/static/admin-help.js web/static/site.js web/static/tracking.js scripts/test-tracking.cjs scripts/test-admin.cjs; do
  node --check "$file"
done
node scripts/test-tracking.cjs
node scripts/test-admin.cjs
for file in scripts/*.sh; do bash -n "$file"; done
python3 scripts/check-content.py
python3 scripts/check-contrast.py
python3 scripts/test-cloud-scripts.py
echo 'All checks passed.'
