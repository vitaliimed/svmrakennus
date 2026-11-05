package site

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCorporateEmailIsOnlySender(t *testing.T) {
	a := testApp(t)
	a.production = true
	t.Setenv("ADMIN_EMAIL", systemSenderEmail)
	t.Setenv("ADMIN_RECOVERY_EMAIL", "person@example.org")
	t.Setenv("ADMIN_BASE_URL", "https://svmrakennus.fi")

	// Old shared-password logins and the company mailbox must not sign in.
	for _, payload := range []string{
		`{"email":"service@svmrakennus.fi","password":"12345"}`,
		`{"email":"person@example.org","password":"12345"}`,
		`{"password":"12345"}`,
	} {
		w := request(a, "POST", "/api/login", strings.NewReader(payload), nil, "")
		if w.Code != 401 {
			t.Fatalf("unexpected production login: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request(a, "POST", "/api/password-link",
		strings.NewReader(`{"email":"service@svmrakennus.fi"}`), nil, ""); w.Code != 400 {
		t.Fatal("system mail cannot register as a user", w.Body.String())
	}
	if w := request(a, "GET", "/admin", nil, nil, ""); w.Code != 200 ||
		!strings.Contains(w.Body.String(), "Your login email") {
		t.Fatal("production admin must ask for an individual email")
	}

	t.Setenv("SMTP_HOST", "smtp.example.org")
	t.Setenv("SMTP_FROM", "different@example.org")
	if err := sendEmail(context.Background(), "person@example.org", "Subject", "Body", ""); err == nil ||
		!strings.Contains(err.Error(), "SMTP_FROM must equal") {
		t.Fatal("different from address was not rejected", err)
	}
	t.Setenv("SMTP_FROM", systemSenderEmail)
	t.Setenv("SMTP_HOST", "")
	if err := sendEmail(context.Background(), "person@example.org", "Subject", "Body", ""); err == nil ||
		!strings.Contains(err.Error(), "SMTP is not configured") {
		t.Fatal("missing SMTP unexpectedly sent email", err)
	}
}

func TestFixedPublicCompanyEmail(t *testing.T) {
	a := testApp(t)
	cookie, csrf := signIn(t, a)
	state, _, err := a.loadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Settings.Email != systemSenderEmail {
		t.Fatal("public email is not corporate sender")
	}
	state.Settings.Email = "someone@example.org"
	payload, _ := json.Marshal(state)
	response := request(a, "PUT", "/api/admin/state", strings.NewReader(string(payload)), cookie, csrf)
	if response.Code != 200 {
		t.Fatal("admin save failed", response.Body.String())
	}
	saved, _, err := a.loadState(context.Background())
	if err != nil || saved.Settings.Email != systemSenderEmail {
		t.Fatal("corporate contact email was changed", saved.Settings.Email, err)
	}
	record, err := a.store.Get(context.Background(), "state/settings")
	if err != nil {
		t.Fatal(err)
	}
	var raw State
	if err := json.Unmarshal(record.Data, &raw); err != nil || raw.Settings.Email != systemSenderEmail {
		t.Fatal("corporate email was persisted incorrectly", err)
	}
}
