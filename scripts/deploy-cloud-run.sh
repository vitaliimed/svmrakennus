#!/usr/bin/env bash
set +x
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
case "${1:-}" in
  --private) access_flags=(--invoker-iam-check --no-allow-unauthenticated) ;;
  --public) access_flags=(--no-invoker-iam-check) ;;
  *) echo 'Usage: bash scripts/deploy-cloud-run.sh --private|--public' >&2; exit 1 ;;
esac
[[ $# -eq 1 ]] || { echo 'Unexpected arguments.' >&2; exit 1; }
[[ -f .deployment.env ]] || { echo 'Run scripts/bootstrap-gcloud.sh first.' >&2; exit 1; }
source .deployment.env
: "${PROJECT_ID:?}" "${SITE_SERVICE:?}" "${SITE_REGION:?}" "${SITE_DATABASE:?}" "${SITE_NAMESPACE:?}" "${SITE_BUCKET:?}" "${SITE_RUNTIME:?}" "${SITE_BUILDER:?}" "${SITE_SESSION_SECRET:?}" "${SITE_PASSWORD_SECRET:?}"
[[ "$SITE_RUNTIME" == "svm-runtime@$PROJECT_ID.iam.gserviceaccount.com" && "$SITE_BUILDER" == "svm-build@$PROJECT_ID.iam.gserviceaccount.com" ]] || { echo 'Deployment config uses unexpected service accounts; rerun bootstrap.' >&2; exit 1; }
bash scripts/verify.sh
gcloud run deploy "$SITE_SERVICE" \
  --source . \
  --project "$PROJECT_ID" \
  --region "$SITE_REGION" \
  --service-account "$SITE_RUNTIME" \
  --build-service-account "projects/$PROJECT_ID/serviceAccounts/$SITE_BUILDER" \
  "${access_flags[@]}" \
  --port 8080 --cpu 1 --memory 1Gi --concurrency 8 \
  --min-instances 0 --max-instances 3 --timeout 120 \
  --update-env-vars "APP_ENV=production,GOOGLE_CLOUD_PROJECT=$PROJECT_ID,FIRESTORE_DATABASE=$SITE_DATABASE,SITE_NAMESPACE=$SITE_NAMESPACE,STORAGE_BUCKET=$SITE_BUCKET" \
  --update-secrets "SESSION_SECRET=$SITE_SESSION_SECRET:latest,ADMIN_PASSWORD=$SITE_PASSWORD_SECRET:latest" \
  --quiet
SITE_URL="$(gcloud run services describe "$SITE_SERVICE" --project "$PROJECT_ID" --region "$SITE_REGION" --format='value(status.url)')"
printf 'Website: %s/fi/\nAdmin: %s/admin\n' "$SITE_URL" "$SITE_URL"
if [[ "$1" == '--private' ]]; then
  printf 'Private preview. In Cloud Shell run:\ngcloud run services proxy %s --project %s --region %s --port 8080\nThen open Web Preview on port 8080.\n' "$SITE_SERVICE" "$PROJECT_ID" "$SITE_REGION"
fi
