#!/usr/bin/env bash
set +x
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[[ -f .deployment.env ]] || { echo 'Run scripts/bootstrap-gcloud.sh first.' >&2; exit 1; }
source .deployment.env
# Publish the SVM owner release only from the tested Git version.
release_flags=()
owner_release=false
if [[ "${1:-}" == "--publish-owner" ]]; then
  [[ "$PROJECT_ID/$SITE_SERVICE/$SITE_REGION" == "project-c6c1d639-9739-4837-9c8/svm-rakennus/europe-north1" ]] || { echo "Unexpected deployment target" >&2; exit 1; }
  owner_release=true
  scope=(--project "$PROJECT_ID" --region "$SITE_REGION")
  origin_url="$(gcloud run services describe "$SITE_SERVICE" "${scope[@]}" --format='value(status.url)')"
  [[ "$origin_url" == https://*.run.app ]] || { echo "Missing service URL" >&2; exit 1; }
  suffix="g$(git rev-parse --short=8 HEAD)-$(date +%s)-$RANDOM"
  revision="$SITE_SERVICE-$suffix"
  release_flags+=(--no-traffic --revision-suffix "$suffix" --tag "$suffix")
  bash scripts/verify.sh
elif (( $# )); then
  echo "Usage: $0 [--publish-owner]" >&2; exit 2
else
  go test ./...
fi
owner_env=""
if $owner_release; then
  public_url="https://svmrakennus.fi"
  # service@svmrakennus.fi is a fixed SMTP sender, never an admin login or recovery destination.
  owner_env=",ADMIN_EMAIL=,ADMIN_RECOVERY_EMAIL=,SMTP_FROM=service@svmrakennus.fi,ADMIN_BASE_URL=$public_url,PUBLIC_SITE_URL=$public_url"
fi
gcloud run deploy "$SITE_SERVICE" \
  --source . "${release_flags[@]}" \
  --project "$PROJECT_ID" \
  --region "$SITE_REGION" \
  --service-account "$SITE_RUNTIME" \
  --build-service-account "projects/$PROJECT_ID/serviceAccounts/$SITE_BUILDER" \
  --allow-unauthenticated \
  --port 8080 --cpu 1 --memory 1Gi --concurrency 8 \
  --min-instances 0 --max-instances 3 --timeout 120 \
  --update-env-vars "APP_ENV=production,GOOGLE_CLOUD_PROJECT=$PROJECT_ID,FIRESTORE_DATABASE=$SITE_DATABASE,SITE_NAMESPACE=$SITE_NAMESPACE,STORAGE_BUCKET=$SITE_BUCKET$owner_env" \
  --update-secrets "SESSION_SECRET=$SITE_SESSION_SECRET:latest,ADMIN_PASSWORD=$SITE_PASSWORD_SECRET:latest"
if $owner_release; then
  describe_service() { gcloud run services describe "$SITE_SERVICE" "${scope[@]}" --format=json; }
  candidate="$(describe_service | python3 -c 'import json,sys
rows=json.load(sys.stdin)["status"]["traffic"]
print(next(r["url"] for r in rows if r.get("tag")==sys.argv[1] and r.get("revisionName")==sys.argv[2]))' "$suffix" "$revision")"
  check_admin() {
    curl -fsS --retry 3 --max-time 45 "$1/admin" | python3 -c 'import sys
s=sys.stdin.read()
assert all(marker in s for marker in ("<html lang=\"en\">","id=\"request-link\"","id=\"redeem-link\"")), "English owner recovery missing"'
  }
  # Cloud Run reserves some URL paths ending in z, including /healthz.
  # Probe /health on new images; keep /admin as an independently checked fallback.
  candidate_healthy=false
  for attempt in {1..12}; do
    check_body="$(curl -fsS --max-time 25 "$candidate/health" 2>/dev/null || true)"
    if [[ "$check_body" == "ok" ]]; then
      candidate_healthy=true
      break
    fi
    if curl -fsS --max-time 25 "$candidate/admin" 2>/dev/null | grep -q 'id="login-form"'; then
      candidate_healthy=true
      break
    fi
    if (( attempt < 12 )); then sleep 5; fi
  done
  if ! $candidate_healthy; then
    echo "Tagged revision failed /health and /admin reachability; existing production traffic unchanged." >&2
    echo "To resume this already-built revision, use scripts/resume-owner-release.sh $revision" >&2
    exit 1
  fi
  bash scripts/smoke-check.sh "$candidate"
  check_admin "$candidate"
  gcloud run services update-traffic "$SITE_SERVICE" "${scope[@]}" --to-revisions "$revision=100" --remove-tags "$suffix"
  describe_service | python3 -c 'import json,sys
rows=json.load(sys.stdin)["status"]["traffic"]
assert sum(r.get("percent",0) for r in rows if r.get("revisionName")==sys.argv[1])==100, "Traffic switch not confirmed"' "$revision"
  check_admin "$origin_url"
  domain_revision() {
    curl -fsS --max-time 20 -D - -o /dev/null "$public_url/admin?release=$suffix" |
      awk 'tolower($1) == "x-svm-revision:" {gsub(/\r/, "", $2); print $2}'
  }
  if [[ "$(domain_revision)" != "$revision" ]]; then
    firebase deploy --project "$PROJECT_ID" --config firebase.svm.json --only hosting
  fi
  for attempt in {1..12}; do
    [[ "$(domain_revision)" == "$revision" ]] && break
    sleep 5
  done
  [[ "$(domain_revision)" == "$revision" ]] || { echo "Cloud Run promoted; check Firebase custom domain and DNS for $public_url" >&2; exit 1; }
  bash scripts/smoke-check.sh "$public_url"
  check_admin "$public_url"
  printf 'Verified %s serves 100%% of traffic. Corporate email sender: service@svmrakennus.fi\n' "$revision"
fi
SITE_URL="$(gcloud run services describe "$SITE_SERVICE" --project "$PROJECT_ID" --region "$SITE_REGION" --format='value(status.url)')"
printf 'Website: %s/\nAdmin: %s/admin\n' "${public_url:-$SITE_URL}" "${public_url:-$SITE_URL}"
echo 'Set your canonical domain in the admin, replace example data and change the initial password.'
