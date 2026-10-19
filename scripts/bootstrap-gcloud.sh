#!/usr/bin/env bash
set +x
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
PROJECT_ID="${1:?Usage: bash scripts/bootstrap-gcloud.sh PROJECT_ID [SERVICE] [REGION]}"
SITE_SERVICE="${2:-svm-rakennus}"
SITE_REGION="${3:-europe-north1}"
[[ "$PROJECT_ID" =~ ^[a-z][a-z0-9-]{4,28}[a-z0-9]$ ]] || { echo 'Invalid project ID' >&2; exit 1; }
[[ "$SITE_SERVICE" =~ ^[a-z][a-z0-9-]{1,20}[a-z0-9]$ ]] || { echo 'Invalid service name' >&2; exit 1; }
SITE_DATABASE="$SITE_SERVICE"
SITE_NAMESPACE="${SITE_SERVICE//-/_}_site"
SITE_BUCKET="${PROJECT_ID}-${SITE_SERVICE}-media"
SITE_RUNTIME="${SITE_SERVICE}-runtime@${PROJECT_ID}.iam.gserviceaccount.com"
SITE_BUILDER="${SITE_SERVICE}-builder@${PROJECT_ID}.iam.gserviceaccount.com"
SITE_SESSION_SECRET="${SITE_SERVICE}-session"
SITE_PASSWORD_SECRET="${SITE_SERVICE}-admin-password"
gcloud services enable run.googleapis.com cloudbuild.googleapis.com artifactregistry.googleapis.com firestore.googleapis.com storage.googleapis.com secretmanager.googleapis.com --project "$PROJECT_ID"
if ! gcloud firestore databases describe --database "$SITE_DATABASE" --project "$PROJECT_ID" >/dev/null 2>&1; then
  gcloud firestore databases create --database "$SITE_DATABASE" --location "$SITE_REGION" --type firestore-native --project "$PROJECT_ID"
fi
if ! gcloud storage buckets describe "gs://$SITE_BUCKET" --project "$PROJECT_ID" >/dev/null 2>&1; then
  gcloud storage buckets create "gs://$SITE_BUCKET" --location "$SITE_REGION" --uniform-bucket-level-access --public-access-prevention --project "$PROJECT_ID"
  gcloud storage buckets update "gs://$SITE_BUCKET" --versioning --project "$PROJECT_ID"
fi
for role_name in runtime builder; do
  sa="${SITE_SERVICE}-${role_name}"
  if ! gcloud iam service-accounts describe "${sa}@${PROJECT_ID}.iam.gserviceaccount.com" --project "$PROJECT_ID" >/dev/null 2>&1; then
    gcloud iam service-accounts create "$sa" --display-name "$SITE_SERVICE $role_name" --project "$PROJECT_ID"
  fi
done
gcloud projects add-iam-policy-binding "$PROJECT_ID" --member "serviceAccount:$SITE_RUNTIME" --role roles/datastore.user --condition=None --quiet >/dev/null
gcloud projects add-iam-policy-binding "$PROJECT_ID" --member "serviceAccount:$SITE_BUILDER" --role roles/run.builder --condition=None --quiet >/dev/null
gcloud storage buckets add-iam-policy-binding "gs://$SITE_BUCKET" --member "serviceAccount:$SITE_RUNTIME" --role roles/storage.objectAdmin --project "$PROJECT_ID" >/dev/null
if ! gcloud secrets describe "$SITE_SESSION_SECRET" --project "$PROJECT_ID" >/dev/null 2>&1; then
  python3 -c 'import secrets; print(secrets.token_hex(48),end="")' | gcloud secrets create "$SITE_SESSION_SECRET" --data-file=- --replication-policy=automatic --project "$PROJECT_ID"
fi
if ! gcloud secrets describe "$SITE_PASSWORD_SECRET" --project "$PROJECT_ID" >/dev/null 2>&1; then
  read -r -s -p 'Initial admin password (Enter = 12345 for initial setup): ' initial_password
  printf '\n'
  initial_password="${initial_password:-12345}"
  printf '%s' "$initial_password" | gcloud secrets create "$SITE_PASSWORD_SECRET" --data-file=- --replication-policy=automatic --project "$PROJECT_ID"
  unset initial_password
fi
for secret_name in "$SITE_SESSION_SECRET" "$SITE_PASSWORD_SECRET"; do
  gcloud secrets add-iam-policy-binding "$secret_name" --member "serviceAccount:$SITE_RUNTIME" --role roles/secretmanager.secretAccessor --project "$PROJECT_ID" >/dev/null
done
umask 077
cat > .deployment.env <<EOF
PROJECT_ID=$PROJECT_ID
SITE_SERVICE=$SITE_SERVICE
SITE_REGION=$SITE_REGION
SITE_DATABASE=$SITE_DATABASE
SITE_NAMESPACE=$SITE_NAMESPACE
SITE_BUCKET=$SITE_BUCKET
SITE_RUNTIME=$SITE_RUNTIME
SITE_BUILDER=$SITE_BUILDER
SITE_SESSION_SECRET=$SITE_SESSION_SECRET
SITE_PASSWORD_SECRET=$SITE_PASSWORD_SECRET
EOF
echo 'Infrastructure ready. Next: bash scripts/deploy-cloud-run.sh'
