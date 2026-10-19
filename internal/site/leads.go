package site

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const uploadLimit = 8 << 20

func validEmail(v string) bool {
	x, e := mail.ParseAddress(v)
	return e == nil && x.Address == v && !strings.ContainsAny(v, "\r\n") && len(v) < 255
}
func (a *App) formToken(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "GET") {
		return
	}
	if !a.rates.allow("token:"+a.clientKey(r), 60, time.Minute) {
		fail(w, 429, "Too many requests")
		return
	}
	jsonOut(w, 200, map[string]string{"token": a.makeToken(session{Exp: time.Now().Add(30 * time.Minute).Unix(), CSRF: randomID()})})
}
func (a *App) addMedia(ctx context.Context, f *multipart.FileHeader, private bool) (Media, error) {
	if f.Size > uploadLimit {
		return Media{}, errors.New("each file must be 8 MB or smaller")
	}
	src, e := f.Open()
	if e != nil {
		return Media{}, e
	}
	defer src.Close()
	b, e := io.ReadAll(io.LimitReader(src, uploadLimit+1))
	if e != nil {
		return Media{}, e
	}
	if len(b) == 0 || len(b) > uploadLimit {
		return Media{}, errors.New("invalid file size")
	}
	typ := http.DetectContentType(b)
	ext := strings.ToLower(filepath.Ext(f.Filename))
	ok := false
	switch typ {
	case "image/jpeg":
		ok = ext == ".jpg" || ext == ".jpeg"
	case "image/png":
		ok = ext == ".png"
	case "image/gif":
		ok = ext == ".gif"
	case "image/webp":
		ok = ext == ".webp"
	case "application/pdf":
		ok = ext == ".pdf"
	}
	if !ok {
		return Media{}, errors.New("allowed files: JPEG, PNG, WebP, GIF and PDF; extension must match the contents")
	}
	if strings.HasPrefix(typ, "image/") {
		if e := checkImage(b, typ); e != nil {
			return Media{}, e
		}
	}
	id := randomID()
	name := filepath.Base(strings.ReplaceAll(f.Filename, "\\", "/"))
	if len(name) > 180 {
		name = name[:180]
	}
	name = cleanLine(name)
	m := Media{ID: id, Name: name, Type: typ, Size: int64(len(b)), Private: private, Created: now(), URL: "/media/" + id}
	if e = a.store.PutFile(ctx, id, b, typ); e != nil {
		return m, e
	}
	raw, _ := json.Marshal(m)
	if _, e = a.store.Put(ctx, "media/"+id, raw, "create"); e != nil {
		_ = a.store.DeleteFile(ctx, id)
		return m, e
	}
	return m, nil
}
func (a *App) deleteMedia(ctx context.Context, m Media) error {
	if e := a.store.DeleteFile(ctx, m.ID); e != nil {
		return e
	}
	return a.store.Delete(ctx, "media/"+m.ID)
}
func (a *App) mediaAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		records, e := a.store.List(r.Context(), "media")
		if e != nil {
			fail(w, 503, "Media list unavailable")
			return
		}
		list := []Media{}
		for _, x := range records {
			var m Media
			if json.Unmarshal(x.Data, &m) == nil && !m.Private {
				list = append(list, m)
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Created > list[j].Created })
		jsonOut(w, 200, list)
	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, uploadLimit+1<<20)
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			fail(w, 400, "Upload too large or invalid")
			return
		}
		defer r.MultipartForm.RemoveAll()
		files := r.MultipartForm.File["file"]
		if len(files) != 1 {
			fail(w, 400, "Upload one file at a time")
			return
		}
		m, e := a.addMedia(r.Context(), files[0], false)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		jsonOut(w, 201, m)
	case "DELETE":
		id := r.URL.Query().Get("id")
		if !regexpMediaID(id) {
			fail(w, 400, "Invalid media ID")
			return
		}
		s, _, e := a.loadState(r.Context())
		if e != nil {
			fail(w, 503, "Storage unavailable")
			return
		}
		raw, _ := json.Marshal(s)
		if bytes.Contains(raw, []byte("/media/"+id)) {
			fail(w, 409, "This file is used on the website; replace its references first")
			return
		}
		rec, e := a.store.Get(r.Context(), "media/"+id)
		if e != nil {
			fail(w, 404, "File not found")
			return
		}
		var m Media
		_ = json.Unmarshal(rec.Data, &m)
		if m.Private {
			fail(w, 403, "Private files are managed with their lead")
			return
		}
		if e = a.deleteMedia(r.Context(), m); e != nil {
			fail(w, 503, "Delete failed")
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 405, "Method not allowed")
	}
}
func regexpMediaID(v string) bool { return len(v) == 48 && strings.Trim(v, "0123456789abcdef") == "" }
func (a *App) serveMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "Method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/media/")
	if !regexpMediaID(id) {
		http.NotFound(w, r)
		return
	}
	rec, e := a.store.Get(r.Context(), "media/"+id)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	var m Media
	if json.Unmarshal(rec.Data, &m) != nil {
		http.NotFound(w, r)
		return
	}
	if m.Private {
		s, _, e := a.loadState(r.Context())
		if e != nil {
			fail(w, 503, "Storage unavailable")
			return
		}
		if _, e = a.session(r, s); e != nil {
			fail(w, 401, "Please sign in to access this file")
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Robots-Tag", "noindex")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	b, e := a.store.File(r.Context(), id)
	if e != nil {
		fail(w, 503, "File unavailable")
		return
	}
	w.Header().Set("Content-Type", m.Type)
	if m.Type == "application/pdf" || m.Private {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": m.Name}))
	}
	http.ServeContent(w, r, m.Name, time.Time{}, bytes.NewReader(b))
}
func (a *App) submitLead(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "POST") {
		return
	}
	if !sameOrigin(r) {
		fail(w, 403, "Origin check failed")
		return
	}
	if !a.rates.allow("lead:"+a.clientKey(r), 5, 10*time.Minute) {
		w.Header().Set("Retry-After", "600")
		fail(w, 429, "Too many requests; try again in 10 minutes")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 26<<20)
	if e := r.ParseMultipartForm(2 << 20); e != nil {
		fail(w, 400, "Request too large or invalid")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if r.FormValue("website") != "" {
		fail(w, 400, "Request rejected")
		return
	}
	token, e := a.readToken(r.FormValue("token"))
	if e != nil || token.Version != 0 || !regexpMediaID(token.CSRF) {
		fail(w, 403, "Form expired; reload the page")
		return
	}
	l := Lead{ID: token.CSRF, Created: now(), Name: strings.TrimSpace(r.FormValue("name")), Company: strings.TrimSpace(r.FormValue("company")), Email: strings.TrimSpace(r.FormValue("email")), Phone: strings.TrimSpace(r.FormValue("phone")), Service: r.FormValue("service"), Message: strings.TrimSpace(r.FormValue("message")), Language: r.FormValue("language"), Status: "new", Files: []Media{}, Campaign: map[string]string{}, Consent: r.FormValue("consent") == "on", EmailStatus: "pending"}
	if l.Language != "fi" {
		l.Language = "en"
	}
	// A signed form token is also an idempotency key: network retries cannot create duplicate leads.
	if _, existingErr := a.store.Get(r.Context(), "leads/"+l.ID); existingErr == nil {
		a.leadSuccess(w, r, l)
		return
	} else if !errors.Is(existingErr, ErrNotFound) {
		fail(w, 503, "Storage unavailable; please retry")
		return
	}
	if len(l.Name) < 2 || len(l.Name) > 150 || len(l.Company) > 150 || !validEmail(l.Email) || len(l.Phone) > 60 || len(l.Message) < 10 || len(l.Message) > 10000 || !l.Consent {
		fail(w, 400, "Please complete your name, valid email, project details (10–10000 characters) and privacy consent")
		return
	}
	s, _, e := a.loadState(r.Context())
	if e != nil {
		fail(w, 503, "Storage unavailable; please try again")
		return
	}
	if l.Service != "" {
		found := false
		for _, v := range s.Services {
			if v.Enabled && v.ID == l.Service {
				found = true
			}
		}
		if !found {
			fail(w, 400, "Please select an available service")
			return
		}
	}
	source := r.FormValue("source")
	if strings.HasPrefix(source, "/") && len(source) < 200 && !strings.ContainsAny(source, "?\r\n") {
		l.Source = source
	}
	for _, k := range []string{"utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term"} {
		v := r.FormValue(k)
		if len(v) > 150 {
			v = v[:150]
		}
		l.Campaign[k] = v
	}
	files := r.MultipartForm.File["files"]
	if len(files) > 3 {
		fail(w, 400, "Maximum 3 attachments")
		return
	}
	if len(files) > 0 && !s.Settings.AllowAttachments {
		fail(w, 400, "Attachments are disabled")
		return
	}
	for _, f := range files {
		m, e := a.addMedia(r.Context(), f, true)
		if e != nil {
			for _, old := range l.Files {
				_ = a.deleteMedia(r.Context(), old)
			}
			fail(w, 400, e.Error())
			return
		}
		l.Files = append(l.Files, m)
	}
	raw, _ := json.Marshal(l)
	if _, e = a.store.Put(r.Context(), "leads/"+l.ID, raw, "create"); e != nil {
		for _, m := range l.Files {
			_ = a.deleteMedia(r.Context(), m)
		}
		if errors.Is(e, ErrConflict) {
			a.leadSuccess(w, r, l)
			return
		}
		fail(w, 503, "Lead was not saved; please retry")
		return
	}
	// Send within the request: Cloud Run may suspend CPU after the response.
	a.attemptEmail(r.Context(), s, l.ID)
	if s.Settings.ReplyConfirmation {
		_ = a.mailer(r.Context(), s, l, true)
	}
	a.leadSuccess(w, r, l)
}
func (a *App) leadSuccess(w http.ResponseWriter, r *http.Request, l Lead) {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		jsonOut(w, 201, map[string]interface{}{"ok": true, "id": l.ID})
		return
	}
	http.Redirect(w, r, "/"+l.Language+"/thank-you/", http.StatusSeeOther)
}
func (a *App) attemptEmail(ctx context.Context, s State, id string) error {
	rec, e := a.store.Get(ctx, "leads/"+id)
	if e != nil {
		return e
	}
	var l Lead
	if e = json.Unmarshal(rec.Data, &l); e != nil {
		return e
	}
	if l.EmailStatus == "sent" {
		return nil
	}
	if l.EmailStatus == "sending" {
		last, _ := time.Parse(time.RFC3339, l.EmailLastAttempt)
		if time.Since(last) < 15*time.Minute {
			return errors.New("email attempt is in progress; wait 15 minutes")
		}
	}
	l.EmailStatus = "sending"
	l.EmailAttempts++
	l.EmailLastAttempt = now()
	raw, _ := json.Marshal(l)
	version, e := a.store.Put(ctx, "leads/"+id, raw, rec.Version)
	if e != nil {
		return e
	}
	sendError := a.mailer(ctx, s, l, false)
	if sendError != nil {
		l.EmailStatus = "failed"
	} else {
		l.EmailStatus = "sent"
	}
	raw, _ = json.Marshal(l)
	_, e = a.store.Put(ctx, "leads/"+id, raw, version)
	if e != nil {
		return fmt.Errorf("email attempted but delivery status could not be saved")
	}
	return sendError
}
func (a *App) listLeads(ctx context.Context) ([]Lead, error) {
	records, e := a.store.List(ctx, "leads")
	if e != nil {
		return nil, e
	}
	out := []Lead{}
	for _, r := range records {
		var l Lead
		if e = json.Unmarshal(r.Data, &l); e != nil {
			return nil, e
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}
func (a *App) leadsAPI(w http.ResponseWriter, r *http.Request, _ State) {
	switch r.Method {
	case "GET":
		out, e := a.listLeads(r.Context())
		if e != nil {
			fail(w, 503, "Leads unavailable")
			return
		}
		jsonOut(w, 200, out)
	case "PATCH":
		var v struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Notes  string `json:"notes"`
		}
		if e := decodeJSON(w, r, &v, 12000); e != nil || !regexpMediaID(v.ID) || len(v.Notes) > 10000 {
			fail(w, 400, "Invalid lead update")
			return
		}
		if v.Status != "new" && v.Status != "contacted" && v.Status != "quoted" && v.Status != "won" && v.Status != "lost" && v.Status != "spam" {
			fail(w, 400, "Invalid status")
			return
		}
		rec, e := a.store.Get(r.Context(), "leads/"+v.ID)
		if e != nil {
			fail(w, 404, "Lead not found")
			return
		}
		var l Lead
		_ = json.Unmarshal(rec.Data, &l)
		l.Status = v.Status
		l.Notes = v.Notes
		raw, _ := json.Marshal(l)
		if _, e = a.store.Put(r.Context(), rec.Key, raw, rec.Version); e != nil {
			fail(w, 409, "Lead changed; refresh and retry")
			return
		}
		jsonOut(w, 200, l)
	case "DELETE":
		id := r.URL.Query().Get("id")
		if !regexpMediaID(id) {
			fail(w, 400, "Invalid lead ID")
			return
		}
		rec, e := a.store.Get(r.Context(), "leads/"+id)
		if e != nil {
			fail(w, 404, "Lead not found")
			return
		}
		var l Lead
		_ = json.Unmarshal(rec.Data, &l)
		for _, m := range l.Files {
			if e = a.deleteMedia(r.Context(), m); e != nil {
				fail(w, 503, "Could not delete an attachment; retry")
				return
			}
		}
		if e = a.store.Delete(r.Context(), rec.Key); e != nil {
			fail(w, 503, "Delete failed")
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 405, "Method not allowed")
	}
}
func csvSafe(v string) string {
	if strings.HasPrefix(v, "=") || strings.HasPrefix(v, "+") || strings.HasPrefix(v, "-") || strings.HasPrefix(v, "@") || strings.HasPrefix(v, "\t") || strings.HasPrefix(v, "\r") {
		return "'" + v
	}
	return v
}
func (a *App) exportLeads(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "GET") {
		return
	}
	leads, e := a.listLeads(r.Context())
	if e != nil {
		fail(w, 503, "Export unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=leads.csv")
	_, _ = w.Write([]byte{0xef, 0xbb, 0xbf})
	c := csv.NewWriter(w)
	_ = c.Write([]string{"ID", "UTC", "Статус", "Ім’я", "Компанія", "Email", "Телефон", "Послуга ID", "Повідомлення", "Нотатки", "Статус email", "Джерело", "Кампанія", "Час у Гельсінкі"})
	for _, l := range leads {
		row := []string{l.ID, l.Created, l.Status, l.Name, l.Company, l.Email, l.Phone, l.Service, l.Message, l.Notes, l.EmailStatus, l.Source, l.Campaign["utm_campaign"], helsinkiTime(l.Created)}
		for i, v := range row {
			row[i] = csvSafe(v)
		}
		_ = c.Write(row)
	}
	c.Flush()
}
func (a *App) retryEmail(w http.ResponseWriter, r *http.Request, s State) {
	if !requireMethod(w, r, "POST") {
		return
	}
	var v struct {
		ID string `json:"id"`
	}
	if e := decodeJSON(w, r, &v, 1000); e != nil || !regexpMediaID(v.ID) {
		fail(w, 400, "Invalid lead ID")
		return
	}
	if e := a.attemptEmail(r.Context(), s, v.ID); e != nil {
		fail(w, 502, e.Error())
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) retention(w http.ResponseWriter, r *http.Request, s State) {
	if !requireMethod(w, r, "POST") {
		return
	}
	if s.Settings.LeadRetentionDays == 0 {
		fail(w, 400, "Set retention days first")
		return
	}
	var v struct {
		Confirm bool `json:"confirm"`
	}
	if e := decodeJSON(w, r, &v, 1000); e != nil || !v.Confirm {
		fail(w, 400, "Explicit confirmation required")
		return
	}
	list, e := a.listLeads(r.Context())
	if e != nil {
		fail(w, 503, "Storage unavailable")
		return
	}
	cut := time.Now().Add(-time.Duration(s.Settings.LeadRetentionDays) * 24 * time.Hour)
	n := 0
	for _, l := range list {
		t, e := time.Parse(time.RFC3339, l.Created)
		if e != nil || !t.Before(cut) {
			continue
		}
		for _, m := range l.Files {
			if e = a.deleteMedia(r.Context(), m); e != nil {
				fail(w, 503, "Attachment cleanup failed after "+strconv.Itoa(n)+" leads")
				return
			}
		}
		if e = a.store.Delete(r.Context(), "leads/"+l.ID); e != nil {
			fail(w, 503, "Cleanup interrupted; retry")
			return
		}
		n++
	}
	jsonOut(w, 200, map[string]int{"deleted": n})
}
