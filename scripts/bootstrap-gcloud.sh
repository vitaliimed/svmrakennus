#!/usr/bin/env bash
set +x
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
PROJECT_ID="${1:?Usage: bash scripts/bootstrap-gcloud.sh PROJECT_ID [SERVICE] [REGION]}"
SITE_SERVICE="${2:-svm-rakennus}"
SITE_REGION="${3:-europe-north1}"
[[ "$PROJECT_ID" =~ ^[a-z][a-z0-9-]{4,28}[a-z0-9]$ ]] || { echo 'Invalid project ID' >&2; exit 1; }
[[ "$SITE_SERVICE" =~ ^[a-z][a-z0-9-]{1,20}[a-z0-9]$ ]] || { echo 'Invalid service name' >&2; exit 1; }
[[ "$SITE_REGION" =~ ^[a-z]+-[a-z]+[0-9]+$ ]] || { echo 'Invalid region' >&2; exit 1; }
SITE_DATABASE="$SITE_SERVICE"
SITE_NAMESPACE="${SITE_SERVICE//-/_}_site"
SITE_BUCKET="${PROJECT_ID}-${SITE_SERVICE}-media"
# These accounts already exist in the client's project. Never create substitutes.
SITE_RUNTIME="svm-runtime@${PROJECT_ID}.iam.gserviceaccount.com"
SITE_BUILDER="svm-build@${PROJECT_ID}.iam.gserviceaccount.com"
SITE_SESSION_SECRET="${SITE_SERVICE}-session"
SITE_PASSWORD_SECRET="${SITE_SERVICE}-admin-password"
command -v gcloud >/dev/null
command -v python3 >/dev/null
[[ "$(gcloud billing projects describe "$PROJECT_ID" --format='value(billingEnabled)')" =~ ^([Tt]rue)$ ]] || { echo 'Project billing is not enabled.' >&2; exit 1; }
for service_account in "$SITE_RUNTIME" "$SITE_BUILDER"; do
  disabled="$(gcloud iam service-accounts describe "$service_account" --project "$PROJECT_ID" --format='value(disabled)')"
  [[ "$disabled" != 'True' && "$disabled" != 'true' ]] || { echo "Disabled service account: $service_account" >&2; exit 1; }
done

gcloud services enable run.googleapis.com cloudbuild.googleapis.com artifactregistry.googleapis.com iam.googleapis.com firestore.googleapis.com storage.googleapis.com secretmanager.googleapis.com --project "$PROJECT_ID"
# List first: a permission/API failure must not be mistaken for a missing database.
databases="$(gcloud firestore databases list --project "$PROJECT_ID" --format=json)"
database_location="$(python3 -c 'import json,sys; d=next((x for x in json.load(sys.stdin) if x["name"].rsplit("/",1)[-1]==sys.argv[1]),None); print(d["locationId"] if d else ""); assert d is None or d.get("type")=="FIRESTORE_NATIVE", "Existing database is not Firestore Native"' "$SITE_DATABASE" <<< "$databases")"
if [[ -z "$database_location" ]]; then
  gcloud firestore databases create --database "$SITE_DATABASE" --location "$SITE_REGION" --type firestore-native --delete-protection --project "$PROJECT_ID"
elif [[ "$database_location" != "$SITE_REGION" ]]; then
  echo "Existing database is in $database_location; expected $SITE_REGION. No replacement was created." >&2
  exit 1
fi
bucket_name="$(gcloud storage buckets list --filter="name=$SITE_BUCKET" --format='value(name)' --project "$PROJECT_ID")"
if [[ -z "$bucket_name" ]]; then
  gcloud storage buckets create "gs://$SITE_BUCKET" --location "$SITE_REGION" --uniform-bucket-level-access --public-access-prevention --project "$PROJECT_ID"
else
  bucket_location="$(gcloud storage buckets describe "gs://$SITE_BUCKET" --format='value(location)' --project "$PROJECT_ID")"
  [[ "${bucket_location,,}" == "$SITE_REGION" ]] || { echo "Existing bucket is in $bucket_location; expected $SITE_REGION." >&2; exit 1; }
fi
gcloud storage buckets update "gs://$SITE_BUCKET" --uniform-bucket-level-access --public-access-prevention --versioning --project "$PROJECT_ID"
gcloud projects add-iam-policy-binding "$PROJECT_ID" --member "serviceAccount:$SITE_RUNTIME" --role roles/datastore.user --condition=None --quiet >/dev/null
gcloud projects add-iam-policy-binding "$PROJECT_ID" --member "serviceAccount:$SITE_BUILDER" --role roles/run.builder --condition=None --quiet >/dev/null
gcloud storage buckets add-iam-policy-binding "gs://$SITE_BUCKET" --member "serviceAccount:$SITE_RUNTIME" --role roles/storage.objectAdmin --project "$PROJECT_ID" >/dev/null

if ! gcloud secrets describe "$SITE_SESSION_SECRET" --project "$PROJECT_ID" >/dev/null 2>&1; then
  python3 -c 'import secrets; print(secrets.token_hex(48),end="")' | gcloud secrets create "$SITE_SESSION_SECRET" --data-file=- --replication-policy=automatic --project "$PROJECT_ID"
fi
if ! gcloud secrets describe "$SITE_PASSWORD_SECRET" --project "$PROJECT_ID" >/dev/null 2>&1; then
  read -r -s -p 'Initial admin password (12-256 characters): ' initial_password
  printf '\n'
  [[ ${#initial_password} -ge 12 && ${#initial_password} -le 256 ]] || { echo 'Use 12-256 characters; no default production password is allowed.' >&2; exit 1; }
  read -r -s -p 'Repeat admin password: ' repeated_password
  printf '\n'
  [[ "$initial_password" == "$repeated_password" ]] || { echo 'Passwords do not match.' >&2; exit 1; }
  printf '%s' "$initial_password" | gcloud secrets create "$SITE_PASSWORD_SECRET" --data-file=- --replication-policy=automatic --project "$PROJECT_ID"
  unset initial_password repeated_password
fi
for secret_name in "$SITE_SESSION_SECRET" "$SITE_PASSWORD_SECRET"; do
  version_state="$(gcloud secrets versions describe latest --secret "$secret_name" --format='value(state)' --project "$PROJECT_ID")"
  [[ "$version_state" == 'ENABLED' ]] || { echo "Latest version of $secret_name is not enabled." >&2; exit 1; }
  gcloud secrets add-iam-policy-binding "$secret_name" --member "serviceAccount:$SITE_RUNTIME" --role roles/secretmanager.secretAccessor --project "$PROJECT_ID" >/dev/null
done
umask 077
deployment_tmp="$(mktemp .deployment.env.XXXXXX)"
trap 'rm -f "$deployment_tmp"' EXIT
cat > "$deployment_tmp" <<EOF
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
mv "$deployment_tmp" .deployment.env
printf 'Infrastructure ready in %s / %s. No Cloud Run service was deployed.\n' "$PROJECT_ID" "$SITE_REGION"
echo 'Next private preview: bash scripts/deploy-cloud-run.sh --private'
