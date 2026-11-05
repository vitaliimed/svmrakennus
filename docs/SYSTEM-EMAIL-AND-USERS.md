# SVM Rakennus: system email and administrator accounts

## Fixed sender (never a user)

All transactional site email is sent **From: service@svmrakennus.fi**.
It is a code-level constant, not editable in Company settings or Users.
The address must be configured as an authorised sender at the SMTP provider.
SMTP_USER/SMTP_PASSWORD belong to the provider and may be different from the
visible sender. Never store SMTP credentials in Git.

The same sender is used for:
- email verification, registration invitations and password reset links;
- enquiry notifications to the configured notification recipient;
- customer enquiry confirmations **when "Email a confirmation to the customer"
  is enabled in Company and contact**;
- SMTP test messages.

The public contact email in Company settings is NOT the mail system's sender.
No user may register or sign in as service@svmrakennus.fi.

## User roles

- **Owner:** one initial Google Cloud–provisioned personal user; can edit the
  website, review enquiries, invite users, approve accounts and promote a
  verified Editor to Admin. Owner role is protected from deletion/demotion in
  the UI.
- **Admin:** a user account promoted by Owner; same site editing privileges,
  can manage Editors, cannot change Owner or another Admin.
- **Editor:** approved user with site editing privileges; cannot manage Users.
- **Pending:** registered and email-verified user awaiting approval. No admin
  access until activated.

No public registration automatically grants Owner or Admin, even when the
email matches any previous ADMIN_EMAIL or ADMIN_RECOVERY_EMAIL value.

## First Owner activation (one-time Cloud Shell operator task)

Run **before** switching production traffic to the new release:

```bash
cd "$HOME/sites/svm-rakennus"
source .deployment.env
python3 scripts/provision-first-owner.py \
  --project "$PROJECT_ID" \
  --database "$SITE_DATABASE" \
  --namespace "$SITE_NAMESPACE" \
  --email "medynikv@gmail.com"
```

This command uses the signed-in Google Cloud IAM account and writes exactly
one initial owner record to Firestore. It does not send email or create a
password. The personal email is the first owner's **login**, not a global
recovery mailbox. Other existing owners block automatic re-provisioning.

After production deployment, open https://svmrakennus.fi/admin, request the
password link with the provisioned personal email, set a password and log in.

To add a client administrator: **Users → Invite a user → client email**.
The client confirms the invitation and creates their own password. Owner then
uses **Make admin** for that verified user. Each user has individual
sessions and independent password reset.

## Deploy and check

```bash
git switch main
git pull --ff-only origin main
bash scripts/verify.sh
bash scripts/deploy-cloud-run.sh --publish-owner
```

The deployment resets the old ADMIN_EMAIL and ADMIN_RECOVERY_EMAIL environment
variables to empty values and enforces SMTP_FROM=service@svmrakennus.fi.
Old shared-password logins/sessions are not accepted in production.

Email delivery still requires correct SMTP credentials and provider
verification of service@svmrakennus.fi; code cannot verify ownership of
that mailbox. Test a registration link, a customer confirmation, and an SMTP
test after deployment. Website settings and leads are not reset by this change.
