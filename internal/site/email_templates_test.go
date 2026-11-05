package site

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestBrandedEmailUpdatesWithWebsiteThemeAndEscapesData(t *testing.T) {
	a := testApp(t)
	s, _, err := a.loadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Settings.BaseURL = "https://svmrakennus.fi"
	s.Settings.Logo = "/static/images/company.png"
	s.Settings.Accent = "#36594b"
	s.Settings.Paper = "#f6f4ee"
	s.Settings.Ink = "#243a30"
	s.Settings.Surface = "#e8ede7"
	s.Settings.HeadingFont = "serif"
	lead := Lead{
		ID: "test-001", Name: "<script>alert('xss')</script>",
		Company: "Example Oy", Email: "customer@example.fi", Phone: "+358401234567",
		Service: "renovation", Message: "<img src=x onerror=alert(1)>\nSecond line",
		Language: "fi", Created: "2026-11-05T11:00:00Z",
		Files:    []Media{{Name: "private-plan.pdf"}},
		Campaign: map[string]string{"utm_campaign": "<script>promo</script>"},
	}

	fi := brandedEmailHTML(s, lead, true)
	admin := brandedEmailHTML(s, lead, false)
	if !strings.Contains(fi, "Kiitos yhteydenotostasi!") ||
		!strings.Contains(fi, "Otamme sinuun pian yhteyttä.") ||
		!strings.Contains(fi, "lang=\"fi\"") {
		t.Fatal("Finnish acknowledgement is incorrect")
	}
	if !strings.Contains(fi, "#36594b") ||
		!strings.Contains(fi, "Georgia") ||
		!strings.Contains(fi, "https://svmrakennus.fi/static/images/company.png") {
		t.Fatal("site branding not reflected in customer email")
	}
	if strings.Contains(fi, "<script>") || strings.Contains(admin, "<script>") ||
		strings.Contains(admin, "<img src=x onerror=") || strings.Contains(admin, "<script>promo</script>") {
		t.Fatal("untrusted data was emitted as HTML")
	}
	for _, field := range []string{"Name", "Company", "Phone", "Email", "Requested service",
		"Message", "Date", "Attachments", "private-plan.pdf", "utm_campaign", "Source"} {
		// Source is omitted when absent, and the date is rendered as a local date.
		if field == "Source" {
			continue
		}
		if !strings.Contains(admin, field) {
			t.Fatalf("missing field in administrator email: %s", field)
		}
	}
	if !strings.Contains(admin, "svmrakennus.fi/admin") {
		t.Fatal("admin login link missing")
	}
	// Admin-uploaded images have a public /media/ URL without a filename extension.
	s.Settings.Logo = "/media/" + strings.Repeat("a", 48)
	uploaded := brandedEmailHTML(s, lead, true)
	if !strings.Contains(uploaded, "https://svmrakennus.fi/media/"+strings.Repeat("a", 48)) {
		t.Fatal("uploaded logo in Media library was not included in branded email")
	}
	s.Settings.Accent = "#e17a36"
	s.Settings.Paper = "#fff4ed"
	s.Settings.HeadingFont = "sans"
	changed := brandedEmailHTML(s, lead, true)
	if !strings.Contains(changed, "#e17a36") || !strings.Contains(changed, "#fff4ed") ||
		!strings.Contains(changed, "font-family:Arial") || changed == fi {
		t.Fatal("newly generated email does not follow updated website theme")
	}
	lead.Language = "en"
	en := brandedEmailHTML(s, lead, true)
	if !strings.Contains(en, "Thank you for your enquiry!") ||
		!strings.Contains(en, "We will contact you shortly.") || !strings.Contains(en, "lang=\"en\"") {
		t.Fatal("English acknowledgement missing")
	}
	if !strings.Contains(smtpEncodedPart("Kiitos ❤️"), base64.StdEncoding.EncodeToString([]byte("Kiitos ❤️"))) {
		t.Fatal("email MIME base64 does not preserve UTF-8")
	}
}

func TestLeadEmailDestinationAndPreviewAreProtected(t *testing.T) {
	a := testApp(t)
	s, _, err := a.loadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Settings.NotificationEmail != adminLeadInbox || !s.Settings.ReplyConfirmation {
		t.Fatal("customer confirmation and fixed lead recipient must be enabled")
	}
	if adminLeadInbox != "svmrakennus@gmail.com" || systemSenderEmail != "service@svmrakennus.fi" {
		t.Fatal("company email identities changed")
	}
	anonymous := request(a, "GET", "/api/admin/email-preview?type=customer&lang=fi", nil, nil, "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatal("email previews must require authenticated admin")
	}
	cookie, csrf := signIn(t, a)
	preview := request(a, "GET", "/api/admin/email-preview?type=customer&lang=fi", nil, cookie, "")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "Kiitos yhteydenotostasi!") {
		t.Fatal("customer preview failed", preview.Body.String())
	}
	if preview.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal("email preview content type incorrect")
	}
	preview = request(a, "GET", "/api/admin/email-preview?type=admin", nil, cookie, "")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "New project enquiry") {
		t.Fatal("admin preview failed")
	}
	if w := request(a, "GET", "/api/admin/email-preview?type=bad", nil, cookie, ""); w.Code != 400 {
		t.Fatal("invalid preview type accepted")
	}

	// These two options are server-owned and must not be changed via admin JSON.
	s.Settings.NotificationEmail = "attacker@example.fi"
	s.Settings.ReplyConfirmation = false
	b, _ := json.Marshal(s)
	w := request(a, "PUT", "/api/admin/state", strings.NewReader(string(b)), cookie, csrf)
	if w.Code != http.StatusOK {
		t.Fatal("settings update should safely ignore restricted mail fields", w.Body.String())
	}
	persisted, _, err := a.loadState(context.Background())
	if err != nil || !persisted.Settings.ReplyConfirmation || persisted.Settings.NotificationEmail != adminLeadInbox {
		t.Fatal("mail configuration was modified", err)
	}
}

func TestPartialMailFailureRetriedWithoutDuplicateAdminAlert(t *testing.T) {
	a := testApp(t)
	adminMessages := 0
	clientMessages := 0
	a.mailer = func(_ context.Context, _ State, _ Lead, customer bool) error {
		if customer {
			clientMessages++
			if clientMessages == 1 {
				return errors.New("simulated customer mailbox failure")
			}
			return nil
		}
		adminMessages++
		return nil
	}
	result := submitTestLead(t, a, false)
	if result.Code != http.StatusCreated {
		t.Fatal("lead was not saved", result.Body.String())
	}
	stored, err := a.listLeads(context.Background())
	if err != nil || len(stored) != 1 ||
		stored[0].EmailStatus != "sent" || stored[0].ConfirmationEmailStatus != "failed" {
		t.Fatal("the two mail statuses were not persisted independently", err)
	}
	cookie, csrf := signIn(t, a)
	body := fmt.Sprintf(`{"id":%q}`, stored[0].ID)
	for attempt := 0; attempt < 2; attempt++ {
		w := request(a, "POST", "/api/admin/retry-email", strings.NewReader(body), cookie, csrf)
		if w.Code != http.StatusOK {
			t.Fatal("retry failed", w.Body.String())
		}
	}
	if adminMessages != 1 || clientMessages != 2 {
		t.Fatalf("duplicate mail on retry: admin=%d, customer=%d", adminMessages, clientMessages)
	}
	stored, err = a.listLeads(context.Background())
	if err != nil || stored[0].ConfirmationEmailStatus != "sent" ||
		stored[0].ConfirmationEmailAttempts != 2 || stored[0].EmailAttempts != 1 {
		t.Fatal("retry results were not saved", err)
	}
}
