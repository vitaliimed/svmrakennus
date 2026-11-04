package site

import (
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Only the server-configured owner can activate or recover administrative access.
func ownerEmail() string { return strings.TrimSpace(os.Getenv("ADMIN_EMAIL")) }

// The recovery destination is chosen by the operator, never by the form.
func recoveryMailbox(owner string) string {
	if address := strings.TrimSpace(os.Getenv("ADMIN_RECOVERY_EMAIL")); address != "" {
		return address
	}
	return owner
}
func recoverableEmail(input, owner, mailbox string) bool {
	input = strings.TrimSpace(input)
	return validEmail(mailbox) && (strings.EqualFold(input, owner) || strings.EqualFold(input, mailbox))
}

func (a *App) passwordLink(owner string, version int, expires int64) string {
	payload := fmt.Sprintf("%d.%d.%s", version, expires, randomID())
	return payload + "." + a.sign([]byte("owner-password:"+strings.ToLower(owner)+"|"+strings.ToLower(recoveryMailbox(owner))+":"+payload))
}

func (a *App) validPasswordLink(token, owner string, version int) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != strconv.Itoa(version) {
		return false
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || expiry <= time.Now().Unix() {
		return false
	}
	want := a.sign([]byte("owner-password:" + strings.ToLower(owner) + "|" + strings.ToLower(recoveryMailbox(owner)) + ":" + strings.Join(parts[:3], ".")))
	return hmac.Equal([]byte(want), []byte(parts[3]))
}

func (a *App) account(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "POST") {
		return
	}
	if !sameOrigin(r) {
		fail(w, 403, "Origin check failed")
		return
	}
	owner := ownerEmail()
	if !validEmail(owner) {
		fail(w, 503, "Owner email access is not configured")
		return
	}
	if !a.rates.allow("account:"+a.clientKey(r), 10, 15*time.Minute) {
		fail(w, 429, "Too many attempts. Try again later.")
		return
	}
	var input struct{ Email, Token, Password string }
	if err := decodeJSON(w, r, &input, 4096); err != nil {
		fail(w, 400, "Invalid request")
		return
	}
	state, version, err := a.loadState(r.Context())
	if err != nil {
		fail(w, 503, "Storage unavailable")
		return
	}
	if r.URL.Path == "/api/password-link" {
		mailbox := recoveryMailbox(owner)
		if recoverableEmail(input.Email, owner, mailbox) && a.rates.allow("owner-link", 3, 15*time.Minute) {
			base, err := url.Parse(strings.TrimSpace(os.Getenv("ADMIN_BASE_URL")))
			if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.Path != "" || base.Fragment != "" || base.RawQuery != "" {
				fail(w, 503, "Set ADMIN_BASE_URL to the HTTPS site origin")
				return
			}
			link := base.String() + "/admin#reset=" + a.passwordLink(owner, state.Auth.Version, time.Now().Add(30*time.Minute).Unix())
			if err := sendEmail(r.Context(), mailbox, "Set your SVM Rakennus password", "Use this link within 30 minutes to set your administrator password:\n\n"+link+"\n\nIf you did not request it, you can ignore this email.", ""); err != nil {
				fail(w, 502, "Email delivery failed. Check SMTP settings.")
				return
			}
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
		return
	}
	if !a.validPasswordLink(input.Token, owner, state.Auth.Version) {
		fail(w, 400, "Invalid or expired link. Request another email.")
		return
	}
	if len(input.Password) == 0 || len(input.Password) > 256 {
		fail(w, 400, "Enter a password (1–256 bytes)")
		return
	}
	state.Auth = Auth{Hash: passwordHash(input.Password), Version: state.Auth.Version + 1, Changed: true}
	state.Revision++
	state.Updated = now()
	b, _ := json.Marshal(state)
	if _, err = a.store.Put(r.Context(), "state/settings", b, version); err != nil {
		fail(w, 409, "State changed. Retry or request a new link.")
		return
	}
	a.cookie(w, r, "", -1)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
