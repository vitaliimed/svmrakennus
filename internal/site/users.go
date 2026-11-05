package site

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Each user is stored separately, outside the public site state and its backups.
// Only a password hash is persisted; actual passwords are never stored or returned.
type adminUser struct {
	Email      string `json:"email"`
	Hash       string `json:"hash"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	Verified   bool   `json:"verified"`
	Version    int    `json:"version"`
	Created    string `json:"created"`
	Updated    string `json:"updated"`
	ResetHash  string `json:"resetHash,omitempty"`
	ResetUntil int64  `json:"resetUntil,omitempty"`
	LinkSentAt int64  `json:"linkSentAt,omitempty"`
}

type adminUserView struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Status   string `json:"status"`
	Verified bool   `json:"verified"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
}

var userResetTokenRE = regexp.MustCompile("^[a-f0-9]{64}\\.[a-f0-9]{48}$")
var userKeyRE = regexp.MustCompile("^[a-f0-9]{64}$")

func normalizedEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
func userKey(email string) string {
	h := sha256.Sum256([]byte(normalizedEmail(email)))
	return hex.EncodeToString(h[:])
}
func ownerIdentity(email string) bool {
	email = normalizedEmail(email)
	if email == "" {
		return false
	}
	return email == normalizedEmail(ownerEmail()) ||
		(os.Getenv("ADMIN_RECOVERY_EMAIL") != "" &&
			email == normalizedEmail(os.Getenv("ADMIN_RECOVERY_EMAIL")))
}
func (a *App) loadUser(ctx context.Context, key string) (adminUser, string, error) {
	if !userKeyRE.MatchString(key) {
		return adminUser{}, "", ErrNotFound
	}
	record, err := a.store.Get(ctx, "users/"+key)
	if err != nil {
		return adminUser{}, "", err
	}
	var user adminUser
	if err := json.Unmarshal(record.Data, &user); err != nil {
		return adminUser{}, "", err
	}
	if !validEmail(user.Email) || userKey(user.Email) != key {
		return adminUser{}, "", errors.New("invalid stored user")
	}
	return user, record.Version, nil
}
func (a *App) saveUser(ctx context.Context, key string, u adminUser, version string) error {
	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	_, err = a.store.Put(ctx, "users/"+key, data, version)
	return err
}
func (a *App) hasActiveOwner(ctx context.Context) bool {
	for _, email := range []string{ownerEmail(), os.Getenv("ADMIN_RECOVERY_EMAIL")} {
		if !validEmail(email) {
			continue
		}
		u, _, err := a.loadUser(ctx, userKey(email))
		if err == nil && u.Role == "owner" && u.Status == "active" && u.Verified && u.Hash != "" {
			return true
		}
	}
	return false
}
func (a *App) loginKnownUser(w http.ResponseWriter, r *http.Request, email, password string) bool {
	email = normalizedEmail(email)
	if !validEmail(email) {
		fail(w, http.StatusUnauthorized, "Incorrect credentials")
		return true
	}
	key := userKey(email)
	u, _, err := a.loadUser(r.Context(), key)
	if errors.Is(err, ErrNotFound) {
		return false
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Account storage unavailable")
		return true
	}
	if u.Hash == "" || !checkPassword(password, u.Hash) {
		fail(w, http.StatusUnauthorized, "Incorrect credentials")
		return true
	}
	if u.Status != "active" || !u.Verified {
		fail(w, http.StatusForbidden, "Account awaiting administrator approval or disabled")
		return true
	}
	sess := session{Exp: time.Now().Add(8 * time.Hour).Unix(), Version: u.Version, CSRF: randomID(), UserID: key}
	a.cookie(w, r, a.makeToken(sess), 8*3600)
	jsonOut(w, http.StatusOK, map[string]string{"csrf": sess.CSRF, "email": u.Email})
	return true
}

func (a *App) sendUserLink(ctx context.Context, email string, invite bool) error {
	email = normalizedEmail(email)
	if !validEmail(email) {
		return errors.New("invalid email")
	}
	if email == systemSenderEmail {
		return errors.New("system email cannot be a user")
	}
	key := userKey(email)
	for attempt := 0; attempt < 4; attempt++ {
		user, version, err := a.loadUser(ctx, key)
		if errors.Is(err, ErrNotFound) {
			user = adminUser{Email: email, Role: "editor", Status: "pending", Created: now()}
			version = "create"
			
		} else if err != nil {
			return err
		}
		if user.Status == "disabled" && !invite {
			return nil
		}
		if invite {
			user.Status = "active"
		}
		// Throttle repeated reset requests across Cloud Run instances.
		if !invite && time.Now().Unix()-user.LinkSentAt < 60 {
			return nil
		}
		secret := randomID()
		digest := sha256.Sum256([]byte(secret))
		user.ResetHash = hex.EncodeToString(digest[:])
		user.ResetUntil = time.Now().Add(30 * time.Minute).Unix()
		user.LinkSentAt = time.Now().Unix()
		user.Updated = now()
		if err = a.saveUser(ctx, key, user, version); errors.Is(err, ErrConflict) {
			continue
		} else if err != nil {
			return err
		}
		base := strings.TrimSpace(os.Getenv("ADMIN_BASE_URL"))
		if base == "" {
			base = strings.TrimSpace(os.Getenv("PUBLIC_SITE_URL"))
		}
		u, err := url.Parse(base)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("ADMIN_BASE_URL must be an HTTPS origin")
		}
		link := base + "/admin#reset=" + key + "." + secret
		body := "Use this SVM Rakennus link to set your password (expires in 30 minutes):\n\n" + link +
			"\n\nAfter saving your password, sign in with this exact email address: " + email +
			"\n\nIf you did not request it, ignore this message."
		return a.accountMailer(ctx, email, "Set your SVM Rakennus password", body, "")
	}
	return ErrConflict
}

func (a *App) userAccount(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "POST") {
		return
	}
	if !sameOrigin(r) {
		fail(w, http.StatusForbidden, "Origin check failed")
		return
	}
	if !a.rates.allow("user-account:"+a.clientKey(r), 10, 15*time.Minute) {
		fail(w, http.StatusTooManyRequests, "Too many attempts. Try again later.")
		return
	}
	var input struct {
		Email    string `json:"email"`
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input, 4096); err != nil {
		fail(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if r.URL.Path == "/api/password-link" {
		if !validEmail(strings.TrimSpace(input.Email)) {
			fail(w, http.StatusBadRequest, "Enter a valid email address")
			return
		}
		if normalizedEmail(input.Email) == systemSenderEmail {
			fail(w, http.StatusBadRequest, "Company mailbox is reserved for outgoing email")
			return
		}
		key := userKey(input.Email)
		if !a.rates.allow("user-link:"+key, 3, 30*time.Minute) {
			jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if err := a.sendUserLink(r.Context(), input.Email, false); err != nil {
			if strings.Contains(err.Error(), "ADMIN_BASE_URL") {
				fail(w, http.StatusServiceUnavailable, err.Error())
			} else {
				fail(w, http.StatusBadGateway, "Unable to send email. Check SMTP settings.")
			}
			return
		}
		jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	// Previously issued single-owner links remain redeemable during migration.
	if strings.Count(input.Token, ".") == 3 {
		if a.production {
			fail(w, http.StatusBadRequest, "Request a new link for an individual account")
			return
		}
		data, _ := json.Marshal(input)
		r.Body = io.NopCloser(bytes.NewReader(data))
		a.account(w, r)
		return
	}
	if !userResetTokenRE.MatchString(input.Token) {
		fail(w, http.StatusBadRequest, "Invalid or expired link. Request another email.")
		return
	}
	if len(input.Password) == 0 || len(input.Password) > 256 {
		fail(w, http.StatusBadRequest, "Enter a password (1–256 bytes)")
		return
	}
	parts := strings.SplitN(input.Token, ".", 2)
	key := parts[0]
	expected := sha256.Sum256([]byte(parts[1]))
	for attempt := 0; attempt < 3; attempt++ {
		user, version, err := a.loadUser(r.Context(), key)
		if err != nil {
			fail(w, http.StatusBadRequest, "Invalid or expired link. Request another email.")
			return
		}
		if user.ResetUntil <= time.Now().Unix() || user.ResetHash == "" ||
			!hmac.Equal([]byte(user.ResetHash), []byte(hex.EncodeToString(expected[:]))) {
			fail(w, http.StatusBadRequest, "Invalid or expired link. Request another email.")
			return
		}
		user.Hash = passwordHash(input.Password)
		user.Verified = true
		user.Version++
		user.ResetHash = ""
		user.ResetUntil = 0
		user.Updated = now()
		if err := a.saveUser(r.Context(), key, user, version); errors.Is(err, ErrConflict) {
			continue
		} else if err != nil {
			fail(w, http.StatusServiceUnavailable, "Account storage unavailable")
			return
		}
		a.cookie(w, r, "", -1)
		jsonOut(w, http.StatusOK, map[string]string{"email": user.Email, "status": user.Status})
		return
	}
	fail(w, http.StatusConflict, "Account changed. Request another link.")
}

func (a *App) changeUserPassword(w http.ResponseWriter, r *http.Request, sess session) {
	if !requireMethod(w, r, "POST") {
		return
	}
	var input struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if decodeJSON(w, r, &input, 4096) != nil || len(input.New) == 0 || len(input.New) > 256 {
		fail(w, http.StatusBadRequest, "Enter a new password (1–256 bytes)")
		return
	}
	u, version, err := a.loadUser(r.Context(), sess.UserID)
	if err != nil || u.Status != "active" || !checkPassword(input.Current, u.Hash) {
		fail(w, http.StatusForbidden, "Current password is incorrect")
		return
	}
	u.Hash = passwordHash(input.New)
	u.Version++
	u.Updated = now()
	if err := a.saveUser(r.Context(), sess.UserID, u, version); err != nil {
		fail(w, http.StatusConflict, "Password change failed; retry")
		return
	}
	a.cookie(w, r, "", -1)
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) userSessionInfo(ctx context.Context, sess session) (string, string) {
	if sess.UserID == "" {
		return ownerEmail(), "owner"
	}
	u, _, err := a.loadUser(ctx, sess.UserID)
	if err != nil {
		return "", ""
	}
	return u.Email, u.Role
}

func (a *App) usersAPI(w http.ResponseWriter, r *http.Request, sess session) {
	_, role := a.userSessionInfo(r.Context(), sess)
	if role != "owner" && role != "admin" {
		fail(w, http.StatusForbidden, "Only administrators can manage users")
		return
	}
	if r.Method == "GET" {
		records, err := a.store.List(r.Context(), "users")
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "Cannot load users")
			return
		}
		out := make([]adminUserView, 0, len(records))
		for _, record := range records {
			key := strings.TrimPrefix(record.Key, "users/")
			var u adminUser
			if json.Unmarshal(record.Data, &u) != nil || !validEmail(u.Email) {
				continue
			}
			out = append(out, adminUserView{ID: key, Email: u.Email, Role: u.Role,
				Status: u.Status, Verified: u.Verified, Created: u.Created, Updated: u.Updated})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
		jsonOut(w, http.StatusOK, out)
		return
	}
	if r.Method == "POST" {
		var input struct {
			Email string `json:"email"`
		}
		if decodeJSON(w, r, &input, 4096) != nil || !validEmail(strings.TrimSpace(input.Email)) {
			fail(w, http.StatusBadRequest, "Enter a valid email address")
			return
		}
		if err := a.sendUserLink(r.Context(), input.Email, true); err != nil {
			fail(w, http.StatusBadGateway, "Could not send invitation email")
			return
		}
		jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if r.Method != "PATCH" {
		fail(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var input struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if decodeJSON(w, r, &input, 4096) != nil || !userKeyRE.MatchString(input.ID) ||
		(input.Action != "approve" && input.Action != "disable" && input.Action != "promote" && input.Action != "demote") {
		fail(w, http.StatusBadRequest, "Invalid user action")
		return
	}
	if input.ID == sess.UserID {
		fail(w, http.StatusForbidden, "You cannot disable or change your own account here")
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		user, version, err := a.loadUser(r.Context(), input.ID)
		if err != nil {
			fail(w, http.StatusNotFound, "User not found")
			return
		}
		if user.Role == "owner" {
			fail(w, http.StatusForbidden, "Owner accounts cannot be disabled")
			return
		}
		if input.Action == "approve" {
			user.Status = "active"
		} else {
			user.Status = "disabled"
		}
		user.Version++ // revoke existing sessions whenever access changes
		user.Updated = now()
		if err := a.saveUser(r.Context(), input.ID, user, version); errors.Is(err, ErrConflict) {
			continue
		} else if err != nil {
			fail(w, http.StatusServiceUnavailable, "Could not update user")
			return
		}
		jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	fail(w, http.StatusConflict, "User changed; reload and retry")
}
