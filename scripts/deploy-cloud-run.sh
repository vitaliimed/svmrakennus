#!/usr/bin/env bash
set +x
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[[ -f .deployment.env ]] || { echo 'Run scripts/bootstrap-gcloud.sh first.' >&2; exit 1; }
source .deployment.env
go test ./...
gcloud run deploy "$SITE_SERVICE" \
  --source . \
  --project "$PROJECT_ID" \
  --region "$SITE_REGION" \
  --service-account "$SITE_RUNTIME" \
  --build-service-account "projects/$PROJECT_ID/serviceAccounts/$SITE_BUILDER" \
  --allow-unauthenticated \
  --port 8080 --cpu 1 --memory 1Gi --concurrency 8 \
  --min-instances 0 --max-instances 3 --timeout 120 \
  --update-env-vars "APP_ENV=production,GOOGLE_CLOUD_PROJECT=$PROJECT_ID,FIRESTORE_DATABASE=$SITE_DATABASE,SITE_NAMESPACE=$SITE_NAMESPACE,STORAGE_BUCKET=$SITE_BUCKET" \
  --update-secrets "SESSION_SECRET=$SITE_SESSION_SECRET:latest,ADMIN_PASSWORD=$SITE_PASSWORD_SECRET:latest"
SITE_URL="$(gcloud run services describe "$SITE_SERVICE" --project "$PROJECT_ID" --region "$SITE_REGION" --format='value(status.url)')"
printf 'Website: %s/fi/\nAdmin: %s/admin\n' "$SITE_URL" "$SITE_URL"
echo 'Set your canonical domain in the admin, replace example data and change the initial password.'
