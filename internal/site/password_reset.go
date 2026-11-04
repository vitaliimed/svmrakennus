package site

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const resetLinkLifetime = 15 * time.Minute
const resetRequestCooldown = 5 * time.Minute

func recoveryHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (a *App) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "POST") {
		return
	}
	if !sameOrigin(r) {
		fail(w, http.StatusForbidden, "Origin check failed")
		return
	}
	// Never accept a recipient from the public request. Only the site owner
	// can configure the recovery destination using Cloud Run secrets/env.
	to := strings.TrimSpace(os.Getenv("ADMIN_RECOVERY_EMAIL"))
	if !validEmail(to) || os.Getenv("SMTP_HOST") == "" || os.Getenv("SMTP_FROM") == "" ||
		os.Getenv("SMTP_USER") == "" || os.Getenv("SMTP_PASSWORD") == "" {
		fail(w, http.StatusServiceUnavailable, "Password recovery email is not configured")
		return
	}
	okResponse := func() { jsonOut(w, 200, map[string]bool{"ok": true}) }
	if !a.rates.allow("password-reset-request:"+a.clientKey(r), 3, time.Hour) {
		okResponse()
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		state, version, err := a.loadState(r.Context())
		if err != nil {
			fail(w, 503, "Storage unavailable")
			return
		}
		u, err := url.Parse(state.Settings.BaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			fail(w, 503, "Configure the public HTTPS domain before enabling password recovery")
			return
		}
		if state.Auth.ResetRequestedAt > 0 &&
			time.Since(time.Unix(state.Auth.ResetRequestedAt, 0)) < resetRequestCooldown {
			okResponse()
			return
		}

		// Only a SHA-256 digest is stored. The random 192-bit token exists
		// briefly in memory and in the email sent to the configured owner.
		token := randomID()
		digest := recoveryHash(token)
		state.Auth.ResetHash = digest
		state.Auth.ResetExpires = time.Now().Add(resetLinkLifetime).Unix()
		state.Auth.ResetRequestedAt = time.Now().Unix()
		state.Revision++
		state.Updated = now()
		payload, _ := json.Marshal(state)
		if _, err = a.store.Put(r.Context(), "state/settings", payload, version); errors.Is(err, ErrConflict) {
			continue
		} else if err != nil {
			fail(w, 503, "Storage unavailable")
			return
		}

		// Fragment identifiers are not sent to the HTTP server or in Referer,
		// unlike a query-string token. JavaScript consumes and removes it.
		link := state.Settings.BaseURL + "/admin#reset=" + token
		if err = a.resetMailer(r.Context(), to, link); err != nil {
			a.clearFailedPasswordReset(r.Context(), digest)
			fail(w, 502, "Recovery email could not be sent; verify Brevo SMTP")
			return
		}
		okResponse()
		return
	}
	fail(w, 503, "Please retry password recovery")
}

func (a *App) clearFailedPasswordReset(ctx context.Context, digest string) {
	state, version, err := a.loadState(ctx)
	if err != nil || state.Auth.ResetHash != digest {
		return
	}
	state.Auth.ResetHash = ""
	state.Auth.ResetExpires = 0
	state.Auth.ResetRequestedAt = 0
	state.Revision++
	state.Updated = now()
	payload, _ := json.Marshal(state)
	_, _ = a.store.Put(ctx, "state/settings", payload, version)
}

func (a *App) confirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "POST") {
		return
	}
	if !sameOrigin(r) {
		fail(w, 403, "Origin check failed")
		return
	}
	if !a.rates.allow("password-reset-confirm:"+a.clientKey(r), 12, 15*time.Minute) {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Too many attempts")
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input, 4096); err != nil ||
		len(input.Token) != 48 || len(input.Password) < 12 || len(input.Password) > 256 {
		fail(w, 400, "Invalid request; password must be 12-256 characters")
		return
	}
	if _, err := hex.DecodeString(input.Token); err != nil {
		fail(w, 400, "Invalid recovery link")
		return
	}
	state, version, err := a.loadState(r.Context())
	if err != nil {
		fail(w, 503, "Storage unavailable")
		return
	}
	digest := recoveryHash(input.Token)
	if state.Auth.ResetHash == "" || time.Now().Unix() >= state.Auth.ResetExpires ||
		!hmac.Equal([]byte(state.Auth.ResetHash), []byte(digest)) {
		fail(w, 400, "Invalid or expired recovery link")
		return
	}
	state.Auth = Auth{
		Hash: passwordHash(input.Password), Version: state.Auth.Version + 1, Changed: true,
	}
	state.Revision++
	state.Updated = now()
	payload, _ := json.Marshal(state)
	if _, err = a.store.Put(r.Context(), "state/settings", payload, version); err != nil {
		if errors.Is(err, ErrConflict) {
			fail(w, 409, "Recovery link was already used or settings changed; try again")
			return
		}
		fail(w, 503, "Storage unavailable")
		return
	}
	// All existing admin sessions become invalid because the auth version changed.
	a.cookie(w, r, "", -1)
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (a *App) sendPasswordReset(ctx context.Context, to, link string) error {
	subject := "SVM Rakennus | Reset your admin password"
	body := "A password reset was requested for the SVM Rakennus website administration.\n\n" +
		"Open this link to set a new password (valid for 15 minutes, single use):\n" +
		link + "\n\nIf you did not request a password reset, you can ignore this email.\n" +
		"Never share this link with anyone.\n"
	return sendSMTP(ctx, to, subject, body, "")
}
