#!/usr/bin/env bash
set -euo pipefail
set +x
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[[ -f .deployment.env ]] || { echo 'Missing .deployment.env' >&2; exit 1; }
source .deployment.env
[[ "${PROJECT_ID}/${SITE_SERVICE}/${SITE_REGION}" == "project-c6c1d639-9739-4837-9c8/svm-rakennus/europe-north1" ]] || {
  echo 'Unexpected Cloud Run release target' >&2; exit 1;
}
[[ "$#" == 1 ]] || { echo "Usage: bash scripts/resume-owner-release.sh REVISION" >&2; exit 2; }
revision="$1"
[[ "$revision" =~ ^svm-rakennus-g[a-z0-9-]{1,55}$ ]] || {
  echo 'Unexpected revision name; stopping' >&2; exit 2;
}
scope=(--project "$PROJECT_ID" --region "$SITE_REGION")
site="https://svmrakennus.fi"
service_json="$(gcloud run services describe "$SITE_SERVICE" "${scope[@]}" --format=json)"
latest_ready="$(printf '%s' "$service_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"].get("latestReadyRevisionName", ""))')"
old_revision="$(printf '%s' "$service_json" | python3 -c '
import json,sys
rows=json.load(sys.stdin)["status"]["traffic"]
old=[r["revisionName"] for r in rows if r.get("percent")==100]
if len(old)!=1: sys.exit("Expected one current 100% revision")
print(old[0])
')"
[[ "$latest_ready" == "$revision" ]] || {
  echo "Requested revision is not latest ready ($latest_ready). No traffic changed." >&2; exit 1;
}
candidate_and_tag="$(printf '%s' "$service_json" | python3 -c '
import json,sys
s=json.load(sys.stdin).get("status",{})
r=sys.argv[1]
for row in s.get("traffic",[]):
    if row.get("revisionName")==r and row.get("tag") and row.get("url"):
        print(row["url"],row["tag"])
        break
' "$revision")"
read -r candidate tag <<<"$candidate_and_tag"
[[ "$candidate" == https://*.run.app && -n "$tag" ]] || {
  echo 'No tagged URL for revision; production traffic unchanged.' >&2; exit 1;
}
origin="$(printf '%s' "$service_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"].get("url",""))')"
[[ "$origin" == https://*.run.app ]] || { echo 'Missing main Cloud Run URL' >&2; exit 1; }
headers="$(mktemp)"
body="$(mktemp)"
promoted=false
release_completed=false
cleanup_and_rollback() {
  exit_code=$?
  trap - EXIT
  if [[ "$promoted" == true && "$release_completed" != true ]]; then
    echo "ROLLBACK: restoring 100% traffic to $old_revision" >&2
    if ! gcloud run services update-traffic "$SITE_SERVICE" "${scope[@]}" --to-revisions "$old_revision=100"; then
      echo "CRITICAL: automatic traffic rollback failed, check Cloud Run immediately." >&2
    fi
  fi
  rm -f "$headers" "$body"
  exit "$exit_code"
}
trap cleanup_and_rollback EXIT
revision_header() {
  awk 'tolower($1)=="x-svm-revision:" {gsub(/\r/, "", $2); print $2}' "$1" | tail -n 1
}
probe() {
  local url="$1"
  : >"$headers"; : >"$body"
  curl -sS --connect-timeout 8 --max-time 25 --dump-header "$headers" --output "$body" \
    --write-out '%{http_code}' "$url/admin" || true
}
echo "Candidate revision: $revision"
echo "Tagged URL: $candidate"
ready=false
for try in {1..12}; do
  code="$(probe "$candidate")"
  got="$(revision_header "$headers")"
  printf 'Admin probe %02d: HTTP=%s, app-revision=%s\n' "$try" "${code:-000}" "${got:-absent}"
  if [[ "$code" == 200 && "$got" == "$revision" ]] && grep -q 'id="login-form"' "$body" && grep -q 'id="redeem-link"' "$body"; then
    ready=true
    break
  fi
  if (( try < 12 )); then sleep 5; fi
done
if ! $ready; then
  echo 'STOP: tagged revision did not serve expected /admin HTML. Production traffic was NOT changed.' >&2
  if [[ "$(revision_header "$headers")" == '' ]]; then
    echo 'Missing X-SVM-Revision header: the 404 may be upstream of the Go app.' >&2
  fi
  gcloud run services describe "$SITE_SERVICE" "${scope[@]}" \
    --format='yaml(status.latestReadyRevisionName,status.traffic,status.conditions)'
  exit 1
fi

bash scripts/smoke-check.sh "$candidate"
curl -fsS --max-time 35 "$candidate/admin" | python3 -c '
import sys
h=sys.stdin.read()
assert all(x in h for x in ("<html lang=\"en\">", "Your login email", "id=\"redeem-link\""))
print("PASS: new administrator sign-in UI")
'
curl -fsS --max-time 35 "$candidate/static/admin.js?v=system-mail-20261105" | python3 -c '
import sys
js=sys.stdin.read()
assert "function usersPage()" in js and "System email · fixed" in js
print("PASS: Users editor and immutable corporate sender")
'

echo "===== Promoting verified revision to 100% ====="
promoted=true
gcloud run services update-traffic "$SITE_SERVICE" "${scope[@]}" \
  --to-revisions "$revision=100" --remove-tags "$tag"
gcloud run services describe "$SITE_SERVICE" "${scope[@]}" --format=json | python3 -c '
import json,sys
rows=json.load(sys.stdin)["status"]["traffic"]
revision=sys.argv[1]
assert sum(r.get("percent",0) for r in rows if r.get("revisionName")==revision)==100
print("PASS: Cloud Run traffic is 100% on",revision)
' "$revision"

check_headers() {
  local url="$1" code got
  for attempt in {1..12}; do
    : >"$headers"
    code="$(curl -sS --max-time 25 --dump-header "$headers" --output /dev/null \
      --write-out '%{http_code}' "$url/admin?release-check=$attempt" || true)"
    got="$(revision_header "$headers")"
    if [[ "$code" == 200 && "$got" == "$revision" ]]; then return 0; fi
    if (( attempt < 12 )); then sleep 5; fi
  done
  return 1
}
if ! check_headers "$origin"; then
  echo 'Cloud Run traffic is 100%, but origin URL does not yet return the expected revision.' >&2
  exit 1
fi
if ! check_headers "$site"; then
  echo 'Production domain still serves a different revision. Updating Firebase Hosting.'
  command -v firebase >/dev/null || {
    echo 'Firebase CLI not installed. Cloud Run is updated, but domain needs attention.' >&2
    exit 1
  }
  firebase deploy --project "$PROJECT_ID" --config firebase.svm.json --only hosting
  check_headers "$site" || {
    echo 'Cloud Run is 100%, but production domain revision cannot be verified.' >&2
    exit 1
  }
fi
bash scripts/smoke-check.sh "$site"
release_completed=true
echo "RELEASE VERIFIED: $revision, Cloud Run 100%, production domain $site"
