package site

import (
    "context"
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
