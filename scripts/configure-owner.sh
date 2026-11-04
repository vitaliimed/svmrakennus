#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
source .deployment.env
site_url=https://svmrakennus.fi
read -r -p 'Administrator email (must have access to this inbox): ' owner_email
read -r -p "Public HTTPS origin [$site_url]: " owner_origin
owner_origin="${owner_origin:-$site_url}"
[[ "$owner_email" =~ ^[[:alnum:]_.%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}$ ]] || { echo 'Invalid email.' >&2; exit 1; }
[[ "$owner_origin" =~ ^https://[a-zA-Z0-9.-]+$ ]] || { echo 'Expected an HTTPS origin, without a trailing slash.' >&2; exit 1; }
gcloud run services update "$SITE_SERVICE" --region "$SITE_REGION" --project "$PROJECT_ID" \
  --update-env-vars "ADMIN_EMAIL=$owner_email,ADMIN_BASE_URL=$owner_origin"
printf 'Open %s/admin and choose Create or reset your password. The link expires after 30 minutes.\n' "$owner_origin"
