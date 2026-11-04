package site

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBuiltinRemovalAndHTML(t *testing.T) {
	a := testApp(t)
	cookie, csrf := signIn(t, a)
	s, _, _ := a.loadState(context.Background())
	s.PageHTML = map[string]string{"/fi/": "<h1>Own content</h1><script>alert(1)</script><img src='/static/images/hero.webp' onerror='alert(2)'>"}
	putState(t, a, s)
	html := request(a, "GET", "/", nil, nil, "").Body.String()
	if !strings.Contains(html, "Own content") || !strings.Contains(html, `id="lead-form"`) || strings.Contains(html, "alert(") {
		t.Fatal("unsafe or incomplete HTML replacement")
	}
	if strings.Contains(request(a, "GET", "/en", nil, nil, "").Body.String(), "Own content") {
		t.Fatal("override leaked between languages")
	}
	const target = "/api/admin/media?asset=/static/images/hero.webp"
	if w := request(a, "DELETE", target, nil, cookie, csrf); w.Code != 409 {
		t.Fatalf("HTML reference conflict: %d", w.Code)
	}
	s.PageHTML = nil
	putState(t, a, s)
	if w := request(a, "DELETE", target, nil, cookie, csrf); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(a, "GET", "/static/images/hero.webp", nil, nil, ""); w.Code != 404 {
		t.Fatal("deleted asset is still public")
	}
	if html := request(a, "GET", "/", nil, nil, "").Body.String(); strings.Contains(html, "/static/images/hero.webp") {
		t.Fatal("dangling asset reference")
	}
}
func TestOwnerPasswordLink(t *testing.T) {
	a := testApp(t)
	old, _ := signIn(t, a)
	t.Setenv("ADMIN_EMAIL", "owner@example.org")
	if w := request(a, "POST", "/api/login", strings.NewReader(`{"password":"12345"}`), nil, ""); w.Code != 401 {
		t.Fatal("bootstrap login allowed")
	}
	s, _, _ := a.loadState(context.Background())
	token := a.passwordLink(ownerEmail(), s.Auth.Version, time.Now().Add(time.Minute).Unix())
	if a.validPasswordLink(token, "other@example.org", s.Auth.Version) {
		t.Fatal("wrong owner accepted")
	}
	if _, err := a.readToken(token); err == nil {
		t.Fatal("reset token accepted as session")
	}
	body := fmt.Sprintf(`{"token":%q,"password":"new-random-password"}`, token)
	if w := request(a, "POST", "/api/set-password", strings.NewReader(body), nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(a, "POST", "/api/set-password", strings.NewReader(body), nil, ""); w.Code != 400 {
		t.Fatal("token reused")
	}
	if w := request(a, "GET", "/api/admin/state", nil, old, ""); w.Code != 401 {
		t.Fatal("previous session still active")
	}
	if w := request(a, "POST", "/api/login", strings.NewReader(`{"email":"owner@example.org","password":"new-random-password"}`), nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}

func TestRecoveryMailboxSeparateFromSignIn(t *testing.T) {
	a := testApp(t)
	owner := "office@example.org"
	t.Setenv("ADMIN_RECOVERY_EMAIL", "support@example.org")
	mailbox := recoveryMailbox(owner)
	if mailbox != "support@example.org" || !recoverableEmail(owner, owner, mailbox) || !recoverableEmail("SUPPORT@example.org", owner, mailbox) || recoverableEmail("stranger@example.org", owner, mailbox) {
		t.Fatal("unexpected recovery identity")
	}
	token := a.passwordLink(owner, 1, time.Now().Add(time.Minute).Unix())
	if !a.validPasswordLink(token, owner, 1) {
		t.Fatal("link rejected")
	}
	t.Setenv("ADMIN_RECOVERY_EMAIL", "new@example.org")
	if a.validPasswordLink(token, owner, 1) {
		t.Fatal("link survived change of recovery inbox")
	}
	t.Setenv("PUBLIC_SITE_URL", "https://svmrakennus.fi")
	html := request(a, "GET", "/en", nil, nil, "").Body.String()
	if !strings.Contains(html, `rel="canonical" href="https://svmrakennus.fi/en"`) {
		t.Fatal("wrong public domain")
	}
}
