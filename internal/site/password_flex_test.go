package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestShortPasswordAllowedAndEmptyRejected(t *testing.T) {
	a := testApp(t)
	t.Setenv("ADMIN_EMAIL", "owner@example.org")
	t.Setenv("ADMIN_BASE_URL", "https://svmrakennus.fi")
	var message string
	a.accountMailer = func(_ context.Context, to, subject, body, reply string) error {
		message = body
		return nil
	}
	// Administrator identity must be provisioned explicitly, not inferred from ADMIN_EMAIL.
	user := adminUser{Email: "owner@example.org", Role: "owner", Status: "active", Created: now(), Version: 1}
	if err := a.saveUser(context.Background(), userKey(user.Email), user, "create"); err != nil {
		t.Fatal(err)
	}
	requestBody := strings.NewReader(`{"email":"owner@example.org"}`)
	w := request(a, "POST", "/api/password-link", requestBody, nil, "")
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	marker := "https://svmrakennus.fi/admin#reset="
	idx := strings.Index(message, marker)
	if idx == -1 {
		t.Fatal("reset link missing")
	}
	token := strings.SplitN(message[idx+len(marker):], "\n", 2)[0]
	for _, tc := range []struct {
		password string
		status   int
	}{{"", 400}, {"a", 200}} {
		body := fmt.Sprintf(`{"token":%q,"password":%q}`, token, tc.password)
		response := request(a, "POST", "/api/set-password", strings.NewReader(body), nil, "")
		if response.Code != tc.status {
			t.Fatalf("password length %d: got HTTP %d, want %d: %s", len(tc.password), response.Code, tc.status, response.Body.String())
		}
	}
	if again := request(a, "POST", "/api/set-password", strings.NewReader(fmt.Sprintf(`{"token":%q,"password":"b"}`, token)), nil, ""); again.Code != 400 {
		t.Fatal("one-time link reused")
	}
	login := request(a, "POST", "/api/login", strings.NewReader(`{"email":"owner@example.org","password":"a"}`), nil, "")
	if login.Code != 200 {
		t.Fatal("short password did not allow login", login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) < 1 {
		t.Fatal("session cookie missing")
	}
	var out map[string]string
	if json.Unmarshal(login.Body.Bytes(), &out) != nil {
		t.Fatal("no session csrf")
	}
	change := request(a, "POST", "/api/admin/password", strings.NewReader(`{"current":"a","new":"b"}`), cookies[0], out["csrf"])
	if change.Code != 200 {
		t.Fatal("change to short password failed", change.Body.String())
	}
	if previous := request(a, "GET", "/api/admin/state", nil, cookies[0], ""); previous.Code != 401 {
		t.Fatal("previous session still active")
	}
	if next := request(a, "POST", "/api/login", strings.NewReader(`{"email":"owner@example.org","password":"b"}`), nil, ""); next.Code != 200 {
		t.Fatal("new short password failed login")
	}
	if invalid := request(a, "POST", "/api/login", strings.NewReader(`{"email":"owner@example.org","password":"a"}`), nil, ""); invalid.Code != 401 {
		t.Fatal("old password remains valid")
	}
	if blank := request(a, "POST", "/api/login", strings.NewReader(`{"email":"owner@example.org","password":""}`), nil, ""); blank.Code == 200 {
		t.Fatal("blank password accepted")
	}
}
