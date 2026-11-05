# Branded enquiry emails — SVM Rakennus

## Recipients and sender

Every **new, valid** enquiry is saved in Firestore before any email delivery is attempted.

| Message | From | To | Language |
| --- | --- | --- | --- |
| New enquiry, full details | `service@svmrakennus.fi` | `svmrakennus@gmail.com` | English administrator layout |
| Thank you, we'll contact you shortly | `service@svmrakennus.fi` | The email entered by the customer | Finnish for `/`, English for `/en` |

The sender and administrator inbox are intentionally fixed in the application. User accounts and their roles are **not** used to choose the SMTP sender.

## Live site branding

Emails use the **saved** theme at send time:
- Accent colour → upper border, brand monogram and CTA button
- Page background / ink / surface colours → outer background, text and information panel
- Heading font (Serif or Sans serif) → subject heading
- Company display name → brand and email footer
- Uploaded PNG/JPEG/WebP/GIF logo under /static/images → header artwork (SVG is intentionally represented by an SVM text mark because common mail clients do not reliably render remote SVG files)

The email is responsive table-based HTML with inline CSS and a text/plain alternative. Customer-provided text and metadata are escaped before rendering.

Theme changes affect **new messages after saving**. Already delivered messages do not change retroactively. Open **Admin → System → Email design preview** to preview Finnish, English and admin designs.

## Delivery and retry

The lead is persisted first. Two separate delivery statuses are recorded on the lead:
`emailStatus` (administrator notification) and `confirmationEmailStatus` (customer acknowledgement), with independent attempts and timestamps.

If one delivery fails, **Enquiries → Open → Retry email** retries failed/pending deliveries. A successful delivery is not repeated. Historical enquiries created before this feature are not automatically sent a first-time customer confirmation on retry.

A successful SMTP response indicates *SMTP server acceptance*, not proof of inbox delivery. Monitor provider logs, spam folders and the recipient mailbox. Both messages depend on a configured SMTP server that permits `service@svmrakennus.fi` as the sender.

## Production release

```bash
cd "$HOME/sites/svm-rakennus"
git switch main && git pull --ff-only origin main
bash scripts/deploy-cloud-run.sh --publish-owner
```

After deployment, test a fresh website enquiry using an email address you control. Confirm both an admin email at `svmrakennus@gmail.com` and a customer acknowledgement. If either is missing, check SMTP configuration and the two statuses in the Enquiries panel.

A read-only preview does not send an actual email or verify SMTP credentials.
