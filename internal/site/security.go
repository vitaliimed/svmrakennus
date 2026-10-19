package site

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

func randomID() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func derive(password string, salt []byte, iterations int) []byte {
	mac := hmac.New(sha256.New, []byte(password))
	mac.Write(salt)
	var block [4]byte
	binary.BigEndian.PutUint32(block[:], 1)
	mac.Write(block[:])
	u := mac.Sum(nil)
	out := append([]byte(nil), u...)
	for i := 1; i < iterations; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}
func passwordHash(password string) string {
	salt := make([]byte, 24)
	if _, e := rand.Read(salt); e != nil {
		panic(e)
	}
	return "pbkdf2-sha256$600000$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(derive(password, salt, 600000))
}
func checkPassword(password, hash string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	n, e := strconv.Atoi(parts[1])
	if e != nil || n < 100000 || n > 1000000 {
		return false
	}
	salt, e := hex.DecodeString(parts[2])
	if e != nil {
		return false
	}
	want, e := hex.DecodeString(parts[3])
	if e != nil {
		return false
	}
	got := derive(password, salt, n)
	return subtle.ConstantTimeCompare(want, got) == 1
}

type session struct {
	Exp     int64  `json:"exp"`
	Version int    `json:"v"`
	CSRF    string `json:"csrf"`
}

func (a *App) sign(b []byte) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write(b)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (a *App) makeToken(s session) string {
	b, _ := json.Marshal(s)
	v := base64.RawURLEncoding.EncodeToString(b)
	return v + "." + a.sign([]byte(v))
}
func (a *App) readToken(v string) (session, error) {
	var s session
	p := strings.Split(v, ".")
	if len(p) != 2 || len(v) > 1500 {
		return s, errors.New("bad token")
	}
	if !hmac.Equal([]byte(a.sign([]byte(p[0]))), []byte(p[1])) {
		return s, errors.New("bad signature")
	}
	b, e := base64.RawURLEncoding.DecodeString(p[0])
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	if e != nil || s.Exp <= time.Now().Unix() {
		return s, errors.New("expired token")
	}
	return s, nil
}
func (a *App) session(r *http.Request, state State) (session, error) {
	c, e := r.Cookie("svm_session")
	if e != nil {
		return session{}, e
	}
	s, e := a.readToken(c.Value)
	if e != nil {
		return s, e
	}
	if s.Version != state.Auth.Version {
		return s, errors.New("session revoked")
	}
	return s, nil
}
func (a *App) cookie(w http.ResponseWriter, r *http.Request, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "svm_session", Value: value, Path: "/", HttpOnly: true, Secure: a.production || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: age})
}
func sameOrigin(r *http.Request) bool {
	v := r.Header.Get("Origin")
	if v == "" {
		v = r.Header.Get("Referer")
	}
	if v == "" {
		return false
	}
	u, e := url.Parse(v)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && strings.EqualFold(u.Host, r.Host)
}
func csrfOK(r *http.Request, s session) bool {
	return sameOrigin(r) && hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF))
}

type rateEntry struct {
	Count int
	Reset time.Time
}
type limiter struct {
	mu    sync.Mutex
	items map[string]rateEntry
}

func (l *limiter) allow(key string, max int, d time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.items == nil {
		l.items = map[string]rateEntry{}
	}
	now := time.Now()
	if len(l.items) > 5000 {
		for k, v := range l.items {
			if now.After(v.Reset) {
				delete(l.items, k)
			}
		}
	}
	v := l.items[key]
	if now.After(v.Reset) {
		v = rateEntry{Reset: now.Add(d)}
	}
	v.Count++
	l.items[key] = v
	return v.Count <= max
}
func (a *App) clientKey(r *http.Request) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if a.production {
		p := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if len(p) >= 2 {
			ip = strings.TrimSpace(p[len(p)-2])
		}
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	return ip
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)
var idRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var colorRE = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func safeFileRef(v string) bool {
	return v == "" || regexp.MustCompile(`^/static/images/[A-Za-z0-9_-]+\.(webp|jpg|jpeg|png|gif|svg)$`).MatchString(v) || regexp.MustCompile(`^/media/[a-f0-9]{48}$`).MatchString(v)
}
func cleanLine(v string) string { return strings.ReplaceAll(strings.ReplaceAll(v, "\r", ""), "\n", "") }
func validateState(s State) error {
	if err := validateRedirects(s.Redirects); err != nil {
		return err
	}
	if s.Schema != 1 {
		return errors.New("unsupported settings schema")
	}
	if s.Settings.MainLanguage != "en" && s.Settings.MainLanguage != "fi" {
		return errors.New("main language must be en or fi")
	}
	if !colorRE.MatchString(s.Settings.Accent) {
		return errors.New("accent must be a hex colour")
	}
	for _, colour := range []string{s.Settings.Paper, s.Settings.Ink, s.Settings.Surface} {
		if colour != "" && !colorRE.MatchString(colour) {
			return errors.New("theme colours must be six-digit hex colours")
		}
	}
	if s.Settings.HeadingFont != "" && s.Settings.HeadingFont != "serif" && s.Settings.HeadingFont != "sans" {
		return errors.New("heading font must be serif or sans")
	}
	if s.Settings.BaseURL != "" {
		u, e := url.Parse(s.Settings.BaseURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return errors.New("site URL must be https://domain without a trailing slash or path")
		}
	}
	for _, v := range []string{s.Settings.Phone, s.Settings.Email, s.Settings.NotificationEmail, s.Settings.Company} {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("single-line field contains a newline")
		}
	}
	if len(s.Settings.Company) > 150 || s.Settings.Company == "" {
		return errors.New("company name is required (max 150 characters)")
	}
	for _, email := range []string{s.Settings.Email, s.Settings.NotificationEmail} {
		if email != "" && !validEmail(email) {
			return errors.New("invalid company or notification email")
		}
	}
	for _, social := range []string{s.Settings.LinkedIn, s.Settings.Facebook, s.Settings.Instagram} {
		if social == "" {
			continue
		}
		u, e := url.Parse(social)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(social) > 2000 {
			return errors.New("social links must be https URLs")
		}
	}
	if s.Settings.Indexing && !s.Settings.Demo && (s.Settings.BaseURL == "" || s.Settings.Email == "" || strings.HasSuffix(s.Settings.Email, "@example.com")) {
		return errors.New("set the real domain and contact email before enabling indexing")
	}
	for _, v := range []string{s.Settings.Logo, s.Settings.Favicon, s.Settings.HeroImage, s.Settings.AboutImage} {
		if !safeFileRef(v) {
			return errors.New("images must be selected from uploaded media or bundled assets")
		}
	}
	if s.Settings.LeadRetentionDays < 0 || s.Settings.LeadRetentionDays > 3650 {
		return errors.New("retention must be between 0 and 3650 days")
	}
	for _, k := range []string{"en", "fi"} {
		if s.Text[k] == nil {
			return errors.New("both language dictionaries are required")
		}
		for _, key := range []string{"hero.title", "nav.contact", "form.submit", "form.success", "form.error", "privacy.title", "cookies.heading", "terms.title"} {
			if s.Text[k][key] == "" {
				return errors.New("required translation is empty: " + k + " / " + key)
			}
		}
	}
	for _, list := range [][]Item{s.Services, s.Projects} {
		if len(list) > 100 {
			return errors.New("maximum 100 items per collection")
		}
		slugs := map[string]bool{}
		ids := map[string]bool{}
		for _, p := range list {
			if !slugRE.MatchString(p.Slug) || slugs[p.Slug] {
				return errors.New("item slug must be unique and contain only lowercase letters, numbers and hyphens")
			}
			if !idRE.MatchString(p.ID) || ids[p.ID] {
				return errors.New("item IDs must be unique")
			}
			slugs[p.Slug] = true
			ids[p.ID] = true
			if !safeFileRef(p.Image) {
				return errors.New("invalid item image")
			}
			for _, g := range p.Gallery {
				if !safeFileRef(g) {
					return errors.New("invalid gallery image")
				}
			}
		}
	}
	for _, d := range s.Documents {
		if !safeFileRef(d.URL) {
			return errors.New("documents must use uploaded media")
		}
	}
	for _, lang := range []string{"en", "fi"} {
		if !safeFileRef(s.SEO[lang].Image) {
			return errors.New("invalid SEO image")
		}
	}
	for _, list := range [][]Item{s.Services, s.Projects} {
		for _, item := range list {
			for _, seo := range item.SEO {
				if !safeFileRef(seo.Image) {
					return errors.New("invalid item SEO image")
				}
			}
		}
	}
	for _, x := range []struct{ v, re string }{{s.Tracking.GA4, `^G-[A-Z0-9]+$`}, {s.Tracking.Ads, `^AW-[0-9]+$`}, {s.Tracking.GTM, `^GTM-[A-Z0-9]+$`}, {s.Tracking.Meta, `^[0-9]+$`}, {s.Tracking.LinkedIn, `^[0-9]+$`}, {s.Tracking.TikTok, `^[a-zA-Z0-9_-]+$`}} {
		if x.v != "" && !regexp.MustCompile(x.re).MatchString(x.v) {
			return errors.New("invalid analytics / advertising ID")
		}
	}
	if s.Tracking.Mode != "direct" && s.Tracking.Mode != "gtm" {
		return errors.New("Google mode must be direct or gtm")
	}
	for key, rule := range s.Tracking.Events {
		if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,50}$`).MatchString(key) {
			return errors.New("invalid event key")
		}
		for _, event := range []string{rule.GA, rule.Meta, rule.TikTok} {
			if event != "" && !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,50}$`).MatchString(event) {
				return errors.New("event names may only contain letters, numbers and underscores")
			}
		}
		if rule.AdsLabel != "" && !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`).MatchString(rule.AdsLabel) {
			return errors.New("invalid Ads conversion label")
		}
		if rule.LinkedIn != "" && !regexp.MustCompile(`^[0-9]{1,30}$`).MatchString(rule.LinkedIn) {
			return errors.New("LinkedIn conversion ID must be numeric")
		}
	}
	if len(s.Blocks) > 20 || len(s.FAQs) > 100 || len(s.Documents) > 100 || len(s.Reviews) > 100 {
		return errors.New("too many content entries")
	}
	known := map[string]bool{"services": true, "about": true, "projects": true, "process": true, "trust": true, "reviews": true, "faq": true, "contact": true}
	seen := map[string]bool{}
	for _, b := range s.Blocks {
		if !known[b.ID] || seen[b.ID] {
			return errors.New("invalid or duplicated section")
		}
		seen[b.ID] = true
	}
	if !seen["contact"] {
		return errors.New("contact section must exist")
	}
	if len(s.Settings.Robots) > 10000 {
		return errors.New("robots text too long")
	}
	raw, _ := json.Marshal(s)
	if len(raw) > 700000 {
		return errors.New("settings exceed 700 KB; shorten text or reduce entries")
	}
	return nil
}
