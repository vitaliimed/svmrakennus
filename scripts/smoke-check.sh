#!/usr/bin/env bash
set +x
set -euo pipefail
SITE_CHECK_URL="${1:?Usage: bash scripts/smoke-check.sh BASE_URL [--authenticated]}"
SITE_CHECK_URL="${SITE_CHECK_URL%/}"
[[ "$SITE_CHECK_URL" =~ ^https://[a-zA-Z0-9.-]+(:[0-9]+)?$ || "$SITE_CHECK_URL" =~ ^http://(localhost|127\.0\.0\.1)(:[0-9]+)?$ ]] || { echo 'Expected an HTTPS base URL or a local HTTP proxy URL, without a path.' >&2; exit 1; }
site_headers=()
if [[ "${2:-}" == '--authenticated' ]]; then
  site_identity_token="$(gcloud auth print-identity-token)"
  site_headers=(-H "Authorization: Bearer $site_identity_token")
elif [[ -n "${2:-}" ]]; then
  echo 'Unknown option; use --authenticated for a private Cloud Run URL.' >&2
  exit 1
fi
for site_path in /healthz /fi/ /en/ /admin /robots.txt /sitemap.xml; do
  site_code="$(curl "${site_headers[@]}" --silent --show-error --output /dev/null --max-time 30 --write-out '%{http_code}' "$SITE_CHECK_URL$site_path")"
  [[ "$site_code" == '200' ]] || { printf 'FAIL %s: HTTP %s\n' "$site_path" "$site_code" >&2; exit 1; }
  printf 'PASS %s: HTTP %s\n' "$site_path" "$site_code"
done
curl "${site_headers[@]}" --fail --silent --show-error --max-time 30 "$SITE_CHECK_URL/fi/" | python3 -c 'import sys; h=sys.stdin.read(); assert "lang=\"fi\"" in h and "rel=\"canonical\"" in h and "SVM Rakennus" in h and "class=\"hero-shade\"" in h; print("PASS Finnish Contrast HTML, company and canonical")'
echo 'Read-only checks passed. Next: admin login, a test enquiry, SMTP and analytics in the real accounts.'
