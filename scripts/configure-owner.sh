#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[[ -f .deployment.env ]] || { echo 'Run scripts/bootstrap-gcloud.sh first.' >&2; exit 1; }
source .deployment.env
read -r -p 'First owner login email (not service@svmrakennus.fi): ' first_owner
[[ -n "$first_owner" ]] || { echo 'Email is required.' >&2; exit 1; }
python3 scripts/provision-first-owner.py \
  --project "$PROJECT_ID" \
  --database "$SITE_DATABASE" \
  --namespace "$SITE_NAMESPACE" \
  --email "$first_owner"
printf '%s\n' 'System sender is always service@svmrakennus.fi.' \
  'The first owner must verify their own inbox and set a password at https://svmrakennus.fi/admin'
