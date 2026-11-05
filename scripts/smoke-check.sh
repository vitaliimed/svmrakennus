#!/usr/bin/env bash
set -euo pipefail
SITE_CHECK_URL="${1:?Usage: bash scripts/smoke-check.sh https://YOUR_SERVICE_URL}"
SITE_CHECK_URL="${SITE_CHECK_URL%/}"
[[ "$SITE_CHECK_URL" =~ ^https://[a-zA-Z0-9.-]+(:[0-9]+)?$ ]] || { echo 'Expected an HTTPS base URL without a path.' >&2; exit 1; }
curl --fail --silent --show-error --max-time 30 "$SITE_CHECK_URL/admin" | python3 -c 'import sys; h=sys.stdin.read(); assert "<html lang=" in h and "id=" in h and "login-form" in h; print("PASS app reachability via /admin (avoids reserved /healthz)")'
for site_path in / /en /admin /robots.txt /sitemap.xml; do
  site_code="$(curl --silent --show-error --output /dev/null --max-time 30 --write-out '%{http_code}' "$SITE_CHECK_URL$site_path")"
  [[ "$site_code" == '200' ]] || { printf 'FAIL %s: HTTP %s\n' "$site_path" "$site_code" >&2; exit 1; }
  printf 'PASS %s: HTTP %s\n' "$site_path" "$site_code"
done
curl --fail --silent --show-error --max-time 30 "$SITE_CHECK_URL/" | python3 -c 'import sys; h=sys.stdin.read(); assert "lang=\"fi\"" in h and "rel=\"canonical\"" in h and "SVM Rakennus" in h; print("PASS Finnish HTML, company and canonical")'
curl -fsS --max-time 30 "$SITE_CHECK_URL/en" | python3 -c 'import sys; assert "<html lang=\"en\">" in sys.stdin.read()'
legacy_code="$(curl --silent --show-error --output /dev/null --max-time 30 --write-out '%{http_code} %{redirect_url}' "$SITE_CHECK_URL/fi/")"
[[ "$legacy_code" == "301 $SITE_CHECK_URL/" ]] || { echo "FAIL /fi/: expected 301, got $legacy_code" >&2; exit 1; }
echo 'PASS /fi/ -> / (301)'
echo 'Read-only checks passed. Next: check admin login, a test enquiry, SMTP and real analytics events in your accounts.'
