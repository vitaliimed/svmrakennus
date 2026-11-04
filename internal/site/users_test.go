package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestIndependentEmailAccountsAndOwnerApproval(t *testing.T) {
	a := testApp(t)
	t.Setenv("ADMIN_EMAIL", "service@svmrakennus.fi")
	t.Setenv("ADMIN_RECOVERY_EMAIL", "owner@example.org")
	t.Setenv("ADMIN_BASE_URL", "https://svmrakennus.fi")
	sent := map[string]string{}
	a.accountMailer = func(_ context.Context, to, subject, body, reply string) error {
		sent[to] = body
		return nil
	}
	requestLink := func(email string) string {
		t.Helper()
		body := fmt.Sprintf(`{"email":%q}`, email)
		w := request(a, "POST", "/api/password-link", strings.NewReader(body), nil, "")
		if w.Code != 200 {
			t.Fatalf("request link for %s: %d %s", email, w.Code, w.Body.String())
		}
		prefix := "https://svmrakennus.fi/admin#reset="
		start := strings.Index(sent[email], prefix)
		if start == -1 {
			t.Fatalf("no email delivered to %s", email)
		}
		return strings.SplitN(sent[email][start+len(prefix):], "\n", 2)[0]
	}
	setPassword := func(token, password string, want int) string {
		t.Helper()
		body := fmt.Sprintf(`{"token":%q,"password":%q}`, token, password)
		w := request(a, "POST", "/api/set-password", strings.NewReader(body), nil, "")
		if w.Code != want {
			t.Fatalf("set password: got %d, want %d: %s", w.Code, want, w.Body.String())
		}
		return w.Body.String()
	}
	login := func(email, password string, want int) (*http.Cookie, string) {
		t.Helper()
		body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
		w := request(a, "POST", "/api/login", strings.NewReader(body), nil, "")
		if w.Code != want {
			t.Fatalf("login %s: got %d, want %d: %s", email, w.Code, want, w.Body.String())
		}
		if want != 200 {
			return nil, ""
		}
		var out map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return w.Result().Cookies()[0], out["csrf"]
	}

	ownerToken := requestLink("owner@example.org")
	if text := setPassword(ownerToken, "owner-password-strong", 200); !strings.Contains(text, `"status":"active"`) {
		t.Fatal("configured recovery owner should be active", text)
	}
	setPassword(ownerToken, "another-password-strong", 400)
	ownerCookie, ownerCSRF := login("owner@example.org", "owner-password-strong", 200)
	login("service@svmrakennus.fi", "owner-password-strong", 401)
	if w := request(a, "GET", "/api/admin/state", nil, ownerCookie, ""); w.Code != 200 {
		t.Fatal("owner could not access admin state", w.Body.String())
	}

	staffToken := requestLink("staff@example.org")
	if text := setPassword(staffToken, "staff-password-strong", 200); !strings.Contains(text, `"status":"pending"`) {
		t.Fatal("public registration must require owner approval", text)
	}
	login("staff@example.org", "staff-password-strong", 403)
	userID := userKey("staff@example.org")
	users := request(a, "GET", "/api/admin/users", nil, ownerCookie, "")
	if users.Code != 200 || !strings.Contains(users.Body.String(), "staff@example.org") ||
		strings.Contains(users.Body.String(), "pbkdf2") || strings.Contains(users.Body.String(), "resetHash") {
		t.Fatal("users directory leaked secret or failed", users.Body.String())
	}
	approve := fmt.Sprintf(`{"id":%q,"action":"approve"}`, userID)
	if w := request(a, "PATCH", "/api/admin/users", strings.NewReader(approve), ownerCookie, ownerCSRF); w.Code != 200 {
		t.Fatal("approval failed", w.Body.String())
	}
	staffCookie, staffCSRF := login("staff@example.org", "staff-password-strong", 200)
	if w := request(a, "GET", "/api/admin/users", nil, staffCookie, ""); w.Code != 403 {
		t.Fatal("staff can read Users directory")
	}
	if w := request(a, "GET", "/api/admin/state", nil, staffCookie, ""); w.Code != 200 {
		t.Fatal("approved user cannot edit site")
	}
	if w := request(a, "POST", "/api/admin/password", strings.NewReader(`{"current":"staff-password-strong","new":"another-staff-password"}`), staffCookie, staffCSRF); w.Code != 200 {
		t.Fatal("per-user change failed", w.Body.String())
	}
	if w := request(a, "GET", "/api/admin/state", nil, staffCookie, ""); w.Code != 401 {
		t.Fatal("password update must revoke previous session")
	}
	login("staff@example.org", "staff-password-strong", 401)
	staffCookie, _ = login("staff@example.org", "another-staff-password", 200)
	login("owner@example.org", "owner-password-strong", 200)
	disable := fmt.Sprintf(`{"id":%q,"action":"disable"}`, userID)
	if w := request(a, "PATCH", "/api/admin/users", strings.NewReader(disable), ownerCookie, ownerCSRF); w.Code != 200 {
		t.Fatal("disable failed", w.Body.String())
	}
	if w := request(a, "GET", "/api/admin/state", nil, staffCookie, ""); w.Code != 401 {
		t.Fatal("disabled user's session must be revoked")
	}
	login("staff@example.org", "another-staff-password", 403)
	if w := request(a, "GET", "/api/admin/users", nil, nil, ""); w.Code != 401 {
		t.Fatal("unauthenticated users directory")
	}
}
