package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type App struct {
	store      Store
	files      fs.FS
	templates  *template.Template
	secret     []byte
	production bool
	rates      limiter
	mailer     func(context.Context, State, Lead, bool) error
}

func New(files fs.FS) (*App, error) {
	a := &App{files: files, production: os.Getenv("K_SERVICE") != "" || os.Getenv("APP_ENV") == "production"}
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir = "data"
	}
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project != "" && os.Getenv("STORAGE_BUCKET") != "" {
		db := os.Getenv("FIRESTORE_DATABASE")
		if db == "" {
			db = "(default)"
		}
		ns := os.Getenv("SITE_NAMESPACE")
		if ns == "" {
			ns = "svm_rakennus_site"
		}
		if !idRE.MatchString(ns) {
			return nil, errors.New("invalid SITE_NAMESPACE")
		}
		a.store = &CloudStore{Project: project, Database: db, Namespace: ns, Bucket: os.Getenv("STORAGE_BUCKET"), Client: &http.Client{Timeout: 25 * time.Second}}
	} else {
		if a.production {
			return nil, errors.New("Cloud Run requires GOOGLE_CLOUD_PROJECT and STORAGE_BUCKET; local disk is ephemeral")
		}
		a.store = &LocalStore{Dir: dir}
	}
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		if a.production {
			return nil, errors.New("SESSION_SECRET is required in production")
		}
		path := filepath.Join(dir, "session-secret")
		b, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			secret = randomID() + randomID()
			if e = atomicWrite(path, []byte(secret)); e != nil {
				return nil, e
			}
		} else if e != nil {
			return nil, e
		} else {
			secret = string(b)
		}
	}
	if len(secret) < 32 {
		return nil, errors.New("SESSION_SECRET must contain at least 32 characters")
	}
	a.secret = []byte(secret)
	funcs := template.FuncMap{"tr": func(s State, l, k string) string {
		if v := s.Text[l][k]; v != "" {
			return v
		}
		return s.Text["en"][k]
	}, "bi": func(b Bilingual, l string) string { return b.Get(l) }, "inc": func(n int) int { return n + 1 }, "enabled": func(v []Item) []Item {
		out := []Item{}
		for _, x := range v {
			if x.Enabled {
				out = append(out, x)
			}
		}
		return out
	}, "paragraphs": func(v string) []string { return strings.Split(v, "\n\n") }, "isimage": func(v string) bool { return strings.HasPrefix(v, "/static/images/") }, "year": func() int { return time.Now().Year() }}
	funcs["seqProcess"] = func() []int { return []int{1, 2, 3, 4} }
	funcs["blockOn"] = func(s State, id string) bool {
		for _, b := range s.Blocks {
			if b.ID == id {
				return b.Enabled
			}
		}
		return false
	}
	funcs["contactHref"] = func(s State, lang string) string {
		for _, b := range s.Blocks {
			if b.ID == "contact" && b.Enabled {
				return "/" + lang + "/#contact"
			}
		}
		return "mailto:" + s.Settings.Email
	}
	var e error
	a.templates, e = template.New("").Funcs(funcs).ParseFS(files, "templates/*.html")
	if e != nil {
		return nil, e
	}
	a.mailer = a.sendNotification
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, e = a.loadState(ctx)
	if errors.Is(e, ErrNotFound) {
		b, e := fs.ReadFile(files, "default.json")
		if e != nil {
			return nil, e
		}
		var s State
		if e = json.Unmarshal(b, &s); e != nil {
			return nil, e
		}
		p := os.Getenv("ADMIN_PASSWORD")
		if p == "" {
			p = "12345"
		}
		if len(p) > 256 || len(p) < 5 {
			return nil, errors.New("invalid initial password length")
		}
		s.Auth = Auth{Hash: passwordHash(p), Version: 1, Changed: p != "12345"}
		s.Updated = now()
		s.Revision = 1
		if e = validateState(s); e != nil {
			return nil, e
		}
		b, _ = json.Marshal(s)
		_, e = a.store.Put(ctx, "state/settings", b, "create")
		if e != nil && !errors.Is(e, ErrConflict) {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	}
	return a, nil
}
func (a *App) loadState(ctx context.Context) (State, string, error) {
	r, e := a.store.Get(ctx, "state/settings")
	if e != nil {
		return State{}, "", e
	}
	var s State
	e = json.Unmarshal(r.Data, &s)
	return s, r.Version, e
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(a.files, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			fail(w, 405, "Method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/robots.txt", a.robots)
	mux.HandleFunc("/sitemap.xml", a.sitemap)
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/images/favicon.svg", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/api/login", a.login)
	mux.HandleFunc("/api/lead", a.submitLead)
	mux.HandleFunc("/api/form-token", a.formToken)
	mux.HandleFunc("/api/admin/", a.adminAPI)
	mux.HandleFunc("/media/", a.serveMedia)
	mux.HandleFunc("/admin", a.adminPage)
	mux.HandleFunc("/admin/", a.adminPage)
	mux.HandleFunc("/", a.publicPage)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://www.googletagmanager.com https://www.googleadservices.com https://googleads.g.doubleclick.net https://connect.facebook.net https://snap.licdn.com https://analytics.tiktok.com https://analytics.us.tiktok.com; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://*.google.com https://*.google-analytics.com https://*.doubleclick.net https://www.facebook.com https://*.linkedin.com https://*.ads.linkedin.com https://*.licdn.com https://*.tiktok.com; connect-src 'self' https://*.google-analytics.com https://*.analytics.google.com https://*.google.com https://*.doubleclick.net https://www.facebook.com https://*.linkedin.com https://*.ads.linkedin.com https://*.tiktok.com; frame-src https://www.googletagmanager.com https://www.google.com https://td.doubleclick.net; font-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		if a.production {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if strings.HasPrefix(r.URL.Path, "/admin") || strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		defer func() {
			if v := recover(); v != nil {
				log.Print("request panic recovered")
				fail(w, 500, "Unexpected error")
			}
		}()
		mux.ServeHTTP(w, r)
	})
}
func jsonOut(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	jsonOut(w, status, map[string]string{"error": message})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v interface{}, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return errors.New("only one JSON object is allowed")
	}
	return nil
}
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		fail(w, 405, "Method not allowed")
		return false
	}
	return true
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "POST") {
		return
	}
	if !sameOrigin(r) {
		fail(w, 403, "Origin check failed")
		return
	}
	if !a.rates.allow("login:"+a.clientKey(r), 10, 15*time.Minute) {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Too many login attempts; retry in 15 minutes")
		return
	}
	var v struct {
		Password string `json:"password"`
	}
	if e := decodeJSON(w, r, &v, 4096); e != nil || len(v.Password) > 256 {
		fail(w, 400, "Invalid request")
		return
	}
	s, _, e := a.loadState(r.Context())
	if e != nil {
		fail(w, 503, "Storage unavailable")
		return
	}
	if !checkPassword(v.Password, s.Auth.Hash) {
		fail(w, 401, "Incorrect password")
		return
	}
	sess := session{Exp: time.Now().Add(8 * time.Hour).Unix(), Version: s.Auth.Version, CSRF: randomID()}
	a.cookie(w, r, a.makeToken(sess), 8*3600)
	jsonOut(w, 200, map[string]string{"csrf": sess.CSRF})
}
func (a *App) adminPage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "GET") {
		return
	}
	if r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if e := a.templates.ExecuteTemplate(w, "admin.html", nil); e != nil {
		log.Print("admin render failed")
	}
}
func (a *App) adminAPI(w http.ResponseWriter, r *http.Request) {
	state, version, e := a.loadState(r.Context())
	if e != nil {
		fail(w, 503, "Storage unavailable")
		return
	}
	sess, e := a.session(r, state)
	if e != nil {
		fail(w, 401, "Please sign in")
		return
	}
	if r.Method != "GET" && !csrfOK(r, sess) {
		fail(w, 403, "CSRF check failed")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/")
	switch path {
	case "session":
		if !requireMethod(w, r, "GET") {
			return
		}
		jsonOut(w, 200, map[string]interface{}{"csrf": sess.CSRF, "weakPassword": !state.Auth.Changed, "storage": fmt.Sprintf("%T", a.store)})
	case "logout":
		if !requireMethod(w, r, "POST") {
			return
		}
		a.cookie(w, r, "", -1)
		jsonOut(w, 200, map[string]bool{"ok": true})
	case "state":
		if r.Method == "GET" {
			state.Auth = Auth{}
			jsonOut(w, 200, state)
			return
		}
		if !requireMethod(w, r, "PUT") {
			return
		}
		var incoming State
		if e = decodeJSON(w, r, &incoming, 750000); e != nil {
			fail(w, 400, "Invalid settings JSON")
			return
		}
		if incoming.Revision != state.Revision {
			fail(w, 409, ErrConflict.Error())
			return
		}
		incoming.Auth = state.Auth
		autoRedirects(state, &incoming)
		incoming.Revision = state.Revision + 1
		incoming.Updated = now()
		if e = validateState(incoming); e != nil {
			fail(w, 400, e.Error())
			return
		}
		b, _ := json.Marshal(incoming)
		_, e = a.store.Put(r.Context(), "state/settings", b, version)
		if errors.Is(e, ErrConflict) {
			fail(w, 409, e.Error())
			return
		}
		if e != nil {
			fail(w, 503, "Save failed; changes have not been published")
			return
		}
		incoming.Auth = Auth{}
		jsonOut(w, 200, incoming)
	case "password":
		if !requireMethod(w, r, "POST") {
			return
		}
		var v struct {
			Current string `json:"current"`
			New     string `json:"new"`
		}
		if e = decodeJSON(w, r, &v, 4096); e != nil {
			fail(w, 400, "Invalid request")
			return
		}
		if !checkPassword(v.Current, state.Auth.Hash) {
			fail(w, 403, "Current password is incorrect")
			return
		}
		if len(v.New) < 12 || len(v.New) > 256 {
			fail(w, 400, "Use 12–256 characters for the new password")
			return
		}
		state.Auth = Auth{Hash: passwordHash(v.New), Version: state.Auth.Version + 1, Changed: true}
		state.Revision++
		state.Updated = now()
		b, _ := json.Marshal(state)
		_, e = a.store.Put(r.Context(), "state/settings", b, version)
		if e != nil {
			fail(w, 409, "Password change failed; reload and retry")
			return
		}
		a.cookie(w, r, "", -1)
		jsonOut(w, 200, map[string]bool{"ok": true})
	case "media":
		a.mediaAPI(w, r)
	case "leads":
		a.leadsAPI(w, r, state)
	case "export":
		a.exportLeads(w, r)
	case "email-test":
		if !requireMethod(w, r, "POST") {
			return
		}
		e = a.mailer(r.Context(), state, Lead{ID: "test", Created: now(), Name: "Тест адміністратора", Message: "Тестове повідомлення SVM Rakennus. Надсилання email працює.", Language: "fi", Files: []Media{}}, false)
		if e != nil {
			fail(w, 502, e.Error())
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	case "backup":
		if !requireMethod(w, r, "GET") {
			return
		}
		state.Auth = Auth{}
		w.Header().Set("Content-Disposition", "attachment; filename=svm-rakennus-settings.json")
		jsonOut(w, 200, state)
	case "retry-email":
		a.retryEmail(w, r, state)
	case "retention":
		a.retention(w, r, state)
	default:
		http.NotFound(w, r)
	}
}
