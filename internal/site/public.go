package site

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type pageData struct {
	State                                                                      State
	Lang, Path, Type, Canonical, EN, FI, DefaultURL, Title, Description, Image string
	NoIndex                                                                    bool
	Item                                                                       Item
	FormToken                                                                  string
	Config                                                                     template.JS
	Schema                                                                     template.JS
	Body                                                                       string
}

// localizedPath returns the canonical URL path: Finnish at /, English at /en.
func localizedPath(lang, tail string) string {
	if lang == "en" {
		if tail == "/" || strings.HasPrefix(tail, "/#") {
			return "/en" + strings.TrimPrefix(tail, "/")
		}
		return "/en" + tail
	}
	return tail
}

// stripFinnishPrefix maps historical /fi/... URLs onto canonical Finnish URLs.
func stripFinnishPrefix(path string) string {
	if path == "/fi" || path == "/fi/" {
		return "/"
	}
	if strings.HasPrefix(path, "/fi/") {
		return strings.TrimPrefix(path, "/fi")
	}
	return path
}

func (a *App) publicPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "Method not allowed")
		return
	}
	s, _, e := a.loadState(r.Context())
	if e != nil {
		fail(w, 503, "Website temporarily unavailable")
		return
	}
	path := r.URL.Path
	// Normalize aliases and slashes only for matching; unknown paths must 404.
	canonicalPath := stripFinnishPrefix(path)
	routePath := canonicalPath
	if routePath != "/" && !strings.HasSuffix(routePath, "/") {
		routePath += "/"
	}
	// Resolve renamed URLs first, to avoid multiple redirect hops.
	for _, redirect := range s.Redirects {
		from := stripFinnishPrefix(redirect.From)
		if routePath != "/" && routePath != "/en/" && routePath == from {
			to := stripFinnishPrefix(redirect.To)
			if to != routePath {
				http.Redirect(w, r, to+querySuffix(r), http.StatusMovedPermanently)
				return
			}
		}
	}

	parts := strings.Split(strings.Trim(routePath, "/"), "/")
	lang := "fi"
	if routePath == "/" {
		parts = nil
	} else if parts[0] == "en" {
		lang = "en"
		parts = parts[1:]
	}
	kind := "home"
	var item Item
	body := ""
	tail := "/"
	if len(parts) == 0 {
		// Canonical home pages are / and /en.
	} else {
		tail = "/" + strings.Join(parts, "/") + "/"
		switch {
		case len(parts) == 2 && (parts[0] == "services" || parts[0] == "projects"):
			kind = parts[0]
			list := s.Services
			if kind == "projects" {
				list = s.Projects
			}
			found := false
			for _, v := range list {
				if v.Enabled && v.Slug == parts[1] {
					item = v
					found = true
					break
				}
			}
			if !found {
				http.NotFound(w, r)
				return
			}
		case len(parts) == 1 && (parts[0] == "privacy" || parts[0] == "cookies" || parts[0] == "terms"):
			kind = parts[0]
			body = s.Text[lang][kind+".body"]
		case len(parts) == 1 && parts[0] == "thank-you":
			kind = "thank-you"
		default:
			http.NotFound(w, r)
			return
		}
	}
	// Redirect a recognized URL only after verifying that the page exists.
	routePath = localizedPath(lang, tail)
	if path != routePath {
		http.Redirect(w, r, routePath+querySuffix(r), http.StatusMovedPermanently)
		return
	}
	base := s.Settings.BaseURL
	if base == "" {
		base = "https://example.com"
	}
	seo := s.SEO[lang]
	if kind == "services" || kind == "projects" {
		seo = item.SEO[lang]
		if seo.Title == "" {
			seo.Title = item.Title.Get(lang) + " | " + s.Settings.Company
		}
		if seo.Description == "" {
			seo.Description = item.Summary.Get(lang)
		}
		if seo.Image == "" {
			seo.Image = item.Image
		}
	} else if kind != "home" {
		seo.Title = s.Text[lang][kind+".title"] + " | " + s.Settings.Company
		seo.Description = s.Text[lang][kind+".summary"]
	}
	if seo.Image == "" {
		seo.Image = s.Settings.HeroImage
	}
	d := pageData{State: s, Lang: lang, Path: tail, Type: kind, Item: item, Body: body, Canonical: base + localizedPath(lang, tail), EN: localizedPath("en", tail), FI: localizedPath("fi", tail), DefaultURL: base + localizedPath("fi", tail), Title: seo.Title, Description: seo.Description, Image: base + seo.Image, NoIndex: !s.Settings.Indexing || s.Settings.Demo || kind == "thank-you" || item.Demo, FormToken: a.makeToken(session{Exp: time.Now().Add(30 * time.Minute).Unix(), CSRF: randomID()})}
	labels := map[string]string{}
	for _, key := range []string{"success", "error", "sending", "submit", "filesError", "rateError", "validationError", "tokenError"} {
		labels[key] = s.Text[lang]["form."+key]
	}
	cfg := map[string]interface{}{"lang": lang, "tracking": s.Tracking, "labels": labels}
	raw, _ := json.Marshal(cfg)
	d.Config = template.JS(raw)
	schema := map[string]interface{}{"@context": "https://schema.org", "@type": "WebPage", "name": seo.Title, "description": seo.Description, "inLanguage": lang, "url": d.Canonical}
	if !s.Settings.Demo {
		schema["publisher"] = map[string]interface{}{"@type": "GeneralContractor", "name": s.Settings.Company, "legalName": s.Settings.LegalName, "url": base, "telephone": s.Settings.Phone, "email": s.Settings.Email, "areaServed": s.Settings.Region, "address": map[string]string{"@type": "PostalAddress", "streetAddress": s.Settings.Address, "addressCountry": s.Settings.Country}}
	}
	if kind == "services" {
		schema["mainEntity"] = map[string]interface{}{"@type": "Service", "name": item.Title.Get(lang), "description": item.Summary.Get(lang), "areaServed": s.Settings.Region}
	}
	raw, _ = json.Marshal(schema)
	d.Schema = template.JS(raw)
	if d.NoIndex {
		w.Header().Set("X-Robots-Tag", "noindex, follow")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == "HEAD" {
		return
	}
	if e = a.templates.ExecuteTemplate(w, "page.html", d); e != nil {
		log.Printf("public render failed: %v", e)
		return
	}
	if a.production {
		// Count served pages by category without sending browser data to a vendor.
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"event": "svm_page_served", "language": lang, "page_type": kind})
	}
}
func (a *App) robots(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "GET") {
		return
	}
	s, _, e := a.loadState(r.Context())
	if e != nil {
		fail(w, 503, "Storage unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if !s.Settings.Indexing || s.Settings.Demo {
		_, _ = fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
		return
	}
	_, _ = fmt.Fprint(w, "User-agent: *\nDisallow: /admin\nDisallow: /api/\n"+s.Settings.Robots+"\n")
	if s.Settings.BaseURL != "" {
		_, _ = fmt.Fprintf(w, "Sitemap: %s/sitemap.xml\n", s.Settings.BaseURL)
	}
}

type siteURL struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod"`
}

func (a *App) sitemap(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "GET") {
		return
	}
	s, _, err := a.loadState(r.Context())
	if err != nil {
		fail(w, 503, "Storage unavailable")
		return
	}
	list := []siteURL{}
	if s.Settings.BaseURL != "" && s.Settings.Indexing && !s.Settings.Demo {
		for _, lang := range []string{"fi", "en"} {
			base := s.Settings.BaseURL + localizedPath(lang, "/")
			list = append(list, siteURL{base, s.Updated})
			for _, group := range []struct {
				kind  string
				items []Item
			}{{"services", s.Services}, {"projects", s.Projects}} {
				for _, p := range group.items {
					if p.Enabled && !p.Demo {
						list = append(list, siteURL{s.Settings.BaseURL + localizedPath(lang, "/"+group.kind+"/"+p.Slug+"/"), s.Updated})
					}
				}
			}
			for _, page := range []string{"privacy", "cookies", "terms"} {
				list = append(list, siteURL{s.Settings.BaseURL + localizedPath(lang, "/"+page+"/"), s.Updated})
			}
		}
	}
	out := struct {
		XMLName xml.Name  `xml:"urlset"`
		XMLNS   string    `xml:"xmlns,attr"`
		URLs    []siteURL `xml:"url"`
	}{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: list}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(out)
}

func querySuffix(r *http.Request) string {
	if r.URL.RawQuery != "" {
		return "?" + r.URL.RawQuery
	}
	return ""
}
