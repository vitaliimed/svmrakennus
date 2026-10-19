#!/usr/bin/env bash
set -euo pipefail
SVM_PROJECT_ID="${1:-project-c6c1d639-9739-4837-9c8}"
SVM_REGION="${2:-europe-north1}"
gcloud auth list --filter='status:ACTIVE' --format='table(account,status)'
gcloud projects describe "$SVM_PROJECT_ID" --format='yaml(name,projectId,projectNumber,parent)'
gcloud projects get-iam-policy "$SVM_PROJECT_ID" --flatten='bindings[].members' --filter='bindings.role=roles/owner' --format='table(bindings.members,bindings.role)'
gcloud billing projects describe "$SVM_PROJECT_ID" --format=json
for SVM_ACCOUNT in svm-build svm-runtime; do
  gcloud iam service-accounts describe "$SVM_ACCOUNT@$SVM_PROJECT_ID.iam.gserviceaccount.com" --project "$SVM_PROJECT_ID" --format='yaml(email,disabled)'
done
gcloud services list --enabled --project "$SVM_PROJECT_ID" --format='table(config.name)'
gcloud run services list --project "$SVM_PROJECT_ID" --region "$SVM_REGION" --format='table(metadata.name,status.url)'
printf '%s\n' 'Read-only check complete. Payment-profile ownership is checked separately by the client in Billing console.'
