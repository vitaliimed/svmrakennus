#!/usr/bin/env bash
set -euo pipefail
SVM_PROJECT_ID="project-c6c1d639-9739-4837-9c8"
gcloud auth list --filter='status:ACTIVE' --format='table(account,status)'
gcloud projects describe "$SVM_PROJECT_ID" --format='yaml(name,projectId,projectNumber,parent)'
gcloud projects get-iam-policy "$SVM_PROJECT_ID" --flatten='bindings[].members' --filter='bindings.role=roles/owner' --format='table(bindings.members,bindings.role)'
gcloud billing projects describe "$SVM_PROJECT_ID" --format=json
SVM_BILLING="$(gcloud billing projects describe "$SVM_PROJECT_ID" --format='value(billingAccountName)')"
if [ -n "$SVM_BILLING" ]; then
  gcloud billing accounts describe "${SVM_BILLING#billingAccounts/}" --format='yaml(name,displayName,open)' || printf '%s\n' 'Billing metadata unavailable. Client must verify payment profile in Billing console.'
fi
gcloud iam service-accounts list --project="$SVM_PROJECT_ID" --format='table(email,disabled)'
gcloud run services list --project="$SVM_PROJECT_ID" --format='table(metadata.name,status.url)'
printf '%s\n' 'Check complete. Confirm client ownership and payment profile in Billing console.'
