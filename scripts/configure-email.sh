#!/usr/bin/env bash
set +x
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[[ -f .deployment.env ]] || { echo 'Run bootstrap first.' >&2; exit 1; }
source .deployment.env
read -r -p 'SMTP host: ' smtp_host
read -r -p 'SMTP port [587]: ' smtp_port
smtp_port="${smtp_port:-587}"
read -r -p 'SMTP login: ' smtp_user
read -r -p 'Verified sender email: ' smtp_from
read -r -s -p 'SMTP password: ' smtp_password
printf '\n'
[[ "$smtp_port" =~ ^(465|587|2525)$ ]] || { echo 'Allowed ports: 465, 587, 2525' >&2; exit 1; }
for value in "$smtp_host" "$smtp_user" "$smtp_from"; do
  [[ -n "$value" && "$value" != *','* && "$value" != *$'\n'* && "$value" != *$'\r'* ]] || { echo 'Values must be nonempty single lines without commas.' >&2; exit 1; }
done
smtp_secret="${SITE_SERVICE}-smtp-password"
if gcloud secrets describe "$smtp_secret" --project "$PROJECT_ID" >/dev/null 2>&1; then
  printf '%s' "$smtp_password" | gcloud secrets versions add "$smtp_secret" --data-file=- --project "$PROJECT_ID"
else
  printf '%s' "$smtp_password" | gcloud secrets create "$smtp_secret" --data-file=- --replication-policy=automatic --project "$PROJECT_ID"
fi
unset smtp_password
gcloud secrets add-iam-policy-binding "$smtp_secret" --member "serviceAccount:$SITE_RUNTIME" --role roles/secretmanager.secretAccessor --project "$PROJECT_ID" >/dev/null
gcloud run services update "$SITE_SERVICE" --project "$PROJECT_ID" --region "$SITE_REGION" \
  --update-env-vars "SMTP_HOST=$smtp_host,SMTP_PORT=$smtp_port,SMTP_USER=$smtp_user,SMTP_FROM=$smtp_from" \
  --update-secrets "SMTP_PASSWORD=$smtp_secret:latest"
echo 'SMTP configured. Set the notification recipient in the admin and send a test email.'
