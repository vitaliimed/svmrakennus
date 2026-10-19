package site

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"svm.rakennus/site/web"
	"sync"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("K_SERVICE", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("STORAGE_BUCKET", "")
	t.Setenv("SESSION_SECRET", strings.Repeat("test-secret-", 5))
	t.Setenv("ADMIN_PASSWORD", "12345")
	t.Setenv("DATA_DIR", t.TempDir())
	a, e := New(web.Files)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func request(a *App, method, path string, body io.Reader, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://example.test"+path, body)
	r.Header.Set("Origin", "http://example.test")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func signIn(t *testing.T, a *App) (*http.Cookie, string) {
	t.Helper()
	w := request(a, "POST", "/api/login", strings.NewReader(`{"password":"12345"}`), nil, "")
	if w.Code != 200 {
		t.Fatalf("login %d: %s", w.Code, w.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Result().Cookies()[0], out["csrf"]
}
func putState(t *testing.T, a *App, s State) {
	t.Helper()
	_, v, e := a.loadState(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(s)
	if _, e = a.store.Put(context.Background(), "state/settings", b, v); e != nil {
		t.Fatal(e)
	}
}
func TestRenderedPagesAndSEO(t *testing.T) {
	a := testApp(t)
	for _, path := range []string{"/en/", "/fi/", "/en/services/firestopping/", "/fi/services/renovation/", "/en/projects/home-renovation/", "/fi/privacy/", "/en/cookies/", "/en/terms/", "/en/thank-you/", "/admin"} {
		w := request(a, "GET", path, nil, nil, "")
		if w.Code != 200 {
			t.Errorf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "</html>") {
			t.Errorf("incomplete HTML: %s", path)
		}
		if dir := os.Getenv("SVM_RENDER_DIR"); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			name := strings.ReplaceAll(strings.Trim(path, "/"), "/", "-")
			if err := os.WriteFile(filepath.Join(dir, name+".html"), w.Body.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if w := request(a, "GET", "/fi/", nil, nil, ""); !strings.Contains(w.Body.String(), `lang="fi"`) || !strings.Contains(w.Body.String(), "Palokatkotyöt") {
		t.Fatal("Finnish translation missing")
	}
	if w := request(a, "GET", "/en/", nil, nil, ""); strings.Contains(w.Body.String(), "ZgotmplZ") {
		t.Fatal("unsafe template context marker")
	}
	if w := request(a, "GET", "/", nil, nil, ""); w.Code != 302 || w.Header().Get("Location") != "/fi/" {
		t.Fatal("main-language redirect")
	}
	if w := request(a, "GET", "/unknown", nil, nil, ""); w.Code != 404 {
		t.Fatal("unknown page must 404")
	}
	if w := request(a, "GET", "/robots.txt", nil, nil, ""); !strings.Contains(w.Body.String(), "Disallow: /") {
		t.Fatal("demo robots should disallow indexing")
	}
	s, _, _ := a.loadState(context.Background())
	s.Settings.Demo = false
	s.Settings.Indexing = true
	s.Settings.BaseURL = "https://build.example.fi"
	s.Text["en"]["hero.title"] = `<script>alert("x")</script>`
	putState(t, a, s)
	w := request(a, "GET", "/en/", nil, nil, "")
	if strings.Contains(w.Body.String(), `<script>alert("x")</script>`) {
		t.Fatal("text XSS not escaped")
	}
	if !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatal("expected escaped text")
	}
	w = request(a, "GET", "/sitemap.xml", nil, nil, "")
	if !strings.Contains(w.Body.String(), "https://build.example.fi/fi/services/firestopping/") || strings.Contains(w.Body.String(), "home-renovation") {
		t.Fatal("sitemap should contain translated services and exclude illustrative projects")
	}
}
func TestAdminAuthenticationCSRFAndPassword(t *testing.T) {
	a := testApp(t)
	if w := request(a, "GET", "/api/admin/state", nil, nil, ""); w.Code != 401 {
		t.Fatal("admin API public")
	}
	r := httptest.NewRequest("POST", "http://example.test/api/login", strings.NewReader(`{"password":"12345"}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin login accepted")
	}
	cookie, csrf := signIn(t, a)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie policy")
	}
	w = request(a, "GET", "/api/admin/state", nil, cookie, "")
	if strings.Contains(w.Body.String(), "pbkdf2") || strings.Contains(w.Body.String(), "test-secret") {
		t.Fatal("secrets exposed")
	}
	if w = request(a, "PUT", "/api/admin/state", strings.NewReader(`{}`), cookie, ""); w.Code != 403 {
		t.Fatal("write without CSRF")
	}
	w = request(a, "POST", "/api/admin/password", strings.NewReader(`{"current":"12345","new":"a-strong-new-password"}`), cookie, csrf)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = request(a, "GET", "/api/admin/state", nil, cookie, ""); w.Code != 401 {
		t.Fatal("old session not revoked")
	}
	s, _, _ := a.loadState(context.Background())
	if checkPassword("12345", s.Auth.Hash) || !checkPassword("a-strong-new-password", s.Auth.Hash) {
		t.Fatal("password update failed")
	}
}
func TestAdminSettingsRejectStaleRevision(t *testing.T) {
	a := testApp(t)
	cookie, csrf := signIn(t, a)
	s, _, _ := a.loadState(context.Background())
	s.Settings.Company = "New Company"
	b, _ := json.Marshal(s)
	w := request(a, "PUT", "/api/admin/state", bytes.NewReader(b), cookie, csrf)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(a, "PUT", "/api/admin/state", bytes.NewReader(b), cookie, csrf)
	if w.Code != 409 {
		t.Fatal("stale revision overwrote content")
	}
	s, _, _ = a.loadState(context.Background())
	if s.Settings.Company != "New Company" {
		t.Fatal("save lost")
	}
	s.Settings.Accent = "red; background: url(https://evil.example)"
	if validateState(s) == nil {
		t.Fatal("CSS injection accepted")
	}
}
func leadBody(t *testing.T, token string, file bool) (*bytes.Buffer, string) {
	t.Helper()
	b := new(bytes.Buffer)
	m := multipart.NewWriter(b)
	for k, v := range map[string]string{"name": "Test Client", "company": "Test Co", "email": "client@example.test", "phone": "+358401234567", "service": "renovation", "message": "Please assess this test renovation project.", "consent": "on", "language": "fi", "token": token, "source": "/fi/"} {
		_ = m.WriteField(k, v)
	}
	if file {
		f, e := m.CreateFormFile("files", "../../plan.png")
		if e != nil {
			t.Fatal(e)
		}
		_ = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	}
	_ = m.Close()
	return b, m.FormDataContentType()
}
func submitTestLead(t *testing.T, a *App, file bool) *httptest.ResponseRecorder {
	t.Helper()
	token := a.makeToken(session{Exp: time.Now().Add(time.Hour).Unix(), CSRF: randomID()})
	b, ctype := leadBody(t, token, file)
	r := httptest.NewRequest("POST", "http://example.test/api/lead", b)
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("Content-Type", ctype)
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func TestLeadSurvivesMailFailureAndPrivateFiles(t *testing.T) {
	a := testApp(t)
	a.mailer = func(context.Context, State, Lead, bool) error { return errors.New("SMTP down") }
	w := submitTestLead(t, a, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	list, e := a.listLeads(context.Background())
	if e != nil || len(list) != 1 || list[0].EmailStatus != "failed" {
		t.Fatal("lead was lost on mail failure")
	}
	l := list[0]
	if len(l.Files) != 1 || l.Files[0].Name != "plan.png" {
		t.Fatal("file name sanitisation")
	}
	w = request(a, "GET", l.Files[0].URL, nil, nil, "")
	if w.Code != 401 {
		t.Fatal("private attachment is public")
	}
	cookie, csrf := signIn(t, a)
	w = request(a, "GET", l.Files[0].URL, nil, cookie, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("authenticated file fetch failed")
	}
	a.mailer = func(context.Context, State, Lead, bool) error { return nil }
	w = request(a, "POST", "/api/admin/retry-email", strings.NewReader(`{"id":"`+l.ID+`"}`), cookie, csrf)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	list, _ = a.listLeads(context.Background())
	if list[0].EmailStatus != "sent" || list[0].EmailAttempts != 2 {
		t.Fatal("retry email status not saved")
	}
	w = request(a, "DELETE", "/api/admin/leads?id="+l.ID, nil, cookie, csrf)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, e = a.store.File(context.Background(), l.Files[0].ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted lead attachment retained")
	}
}
func TestStoreAtomicConflictAndPersistence(t *testing.T) {
	store := &LocalStore{Dir: t.TempDir()}
	ctx := context.Background()
	v, e := store.Put(ctx, "state/settings", []byte("original"), "create")
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, value := range []string{"one", "two"} {
		wg.Add(1)
		go func(x string) { defer wg.Done(); _, e := store.Put(ctx, "state/settings", []byte(x), v); results <- e }(value)
	}
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal("concurrent save lost conflict protection")
	}
	fresh := &LocalStore{Dir: store.Dir}
	if _, e = fresh.Get(ctx, "state/settings"); e != nil {
		t.Fatal("data did not persist")
	}
}
func TestCloudAdapterAndConditionalUpdates(t *testing.T) {
	var tokenCalls int
	var mu sync.Mutex
	var stored cloudDoc
	version := "2026-10-10T12:00:00Z"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/token":
			tokenCalls++
			jsonOut(w, 200, map[string]interface{}{"access_token": "test-token", "expires_in": 3600})
		case strings.HasSuffix(r.URL.Path, "/state/settings"):
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing bearer")
			}
			if r.Method == "PATCH" {
				if r.URL.Query().Get("currentDocument.updateTime") != "" && r.URL.Query().Get("currentDocument.updateTime") != version {
					jsonOut(w, 412, map[string]string{"error": "conflict"})
					return
				}
				_ = json.NewDecoder(r.Body).Decode(&stored)
				stored.UpdateTime = version
			}
			jsonOut(w, 200, stored)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	store := &CloudStore{Project: "project", Database: "(default)", Namespace: "site", Bucket: "bucket", Client: server.Client(), MetadataURL: server.URL + "/token", FirestoreURL: server.URL}
	ctx := context.Background()
	v, e := store.Put(ctx, "state/settings", []byte(`{"name":"test"}`), "create")
	if e != nil || v != version {
		t.Fatal(e, v)
	}
	rec, e := store.Get(ctx, "state/settings")
	if e != nil || string(rec.Data) != `{"name":"test"}` {
		t.Fatal(e, string(rec.Data))
	}
	if _, e = store.Put(ctx, "state/settings", []byte(`{}`), "stale"); !errors.Is(e, ErrConflict) {
		t.Fatal("cloud precondition ignored")
	}
	if tokenCalls != 1 {
		t.Fatal("metadata token not cached")
	}
}
func TestCSVFormulaAndExpiredToken(t *testing.T) {
	if csvSafe("=1+2") != "'=1+2" || csvSafe("normal") != "normal" {
		t.Fatal("CSV formula protection")
	}
	a := testApp(t)
	if _, e := a.readToken(a.makeToken(session{Exp: time.Now().Add(-time.Hour).Unix()})); e == nil {
		t.Fatal("expired session accepted")
	}
	if checkPassword("wrong", passwordHash("correct")) {
		t.Fatal("invalid password accepted")
	}
}

func TestLeadIdempotencyAndImageValidation(t *testing.T) {
	a := testApp(t)
	sent := 0
	a.mailer = func(context.Context, State, Lead, bool) error { sent++; return nil }
	token := a.makeToken(session{Exp: time.Now().Add(time.Hour).Unix(), CSRF: randomID()})
	for i := 0; i < 2; i++ {
		b, ctype := leadBody(t, token, false)
		r := httptest.NewRequest("POST", "http://example.test/api/lead", b)
		r.Header.Set("Origin", "http://example.test")
		r.Header.Set("Content-Type", ctype)
		r.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	list, _ := a.listLeads(context.Background())
	if len(list) != 1 || sent != 1 {
		t.Fatal("network retry created duplicate lead or email")
	}
	for _, file := range []string{"hero", "interior", "terrace", "craft"} {
		b, e := web.Files.ReadFile("static/images/" + file + ".webp")
		if e != nil {
			t.Fatal(e)
		}
		if e = checkImage(b, "image/webp"); e != nil {
			t.Fatal(file, e)
		}
	}
	if checkImage([]byte("\x89PNG\r\n\x1a\ninvalid"), "image/png") == nil {
		t.Fatal("invalid image accepted")
	}
	if safeFileRef("/static/images/../../admin") || safeFileRef("https://evil.example/image.jpg") {
		t.Fatal("unsafe image reference")
	}
}

func TestUploadMediaAndProtectedDelete(t *testing.T) {
	a := testApp(t)
	cookie, csrf := signIn(t, a)
	raw, err := web.Files.ReadFile("static/images/interior.webp")
	if err != nil {
		t.Fatal(err)
	}
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	file, _ := writer.CreateFormFile("file", "interior.webp")
	_, _ = file.Write(raw)
	_ = writer.Close()
	r := httptest.NewRequest("POST", "http://example.test/api/admin/media", body)
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var media Media
	if err = json.Unmarshal(w.Body.Bytes(), &media); err != nil {
		t.Fatal(err)
	}
	s, _, _ := a.loadState(context.Background())
	s.Settings.AboutImage = media.URL
	putState(t, a, s)
	w = request(a, "DELETE", "/api/admin/media?id="+media.ID, nil, cookie, csrf)
	if w.Code != 409 {
		t.Fatal("referenced image was deleted")
	}
	s.Settings.AboutImage = "/static/images/craft.webp"
	putState(t, a, s)
	w = request(a, "DELETE", "/api/admin/media?id="+media.ID, nil, cookie, csrf)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}

func TestAutomaticRedirectAndCycleRejection(t *testing.T) {
	a := testApp(t)
	cookie, csrf := signIn(t, a)
	s, _, _ := a.loadState(context.Background())
	old := s.Services[0].Slug
	s.Services[0].Slug = "updated-renovation"
	raw, _ := json.Marshal(s)
	w := request(a, "PUT", "/api/admin/state", bytes.NewReader(raw), cookie, csrf)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(a, "GET", "/fi/services/"+old+"/", nil, nil, "")
	if w.Code != 301 || w.Header().Get("Location") != "/fi/services/updated-renovation/" {
		t.Fatal("old service URL did not redirect")
	}
	if validateRedirects([]Redirect{{"/en/", "/fi/"}, {"/fi/", "/en/"}}) == nil {
		t.Fatal("redirect cycle accepted")
	}
}
