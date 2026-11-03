package site

import (
	"context"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestCanonicalLanguageRoutesAndInternalLinks(t *testing.T) {
	a := testApp(t)
	s, _, err := a.loadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Settings.BaseURL = "https://svmrakennus.fi"
	s.Settings.Demo = false
	s.Settings.Indexing = true
	putState(t, a, s)

	for _, tc := range []struct {
		path, lang, canonical, other string
	}{
		{"/", "fi", "https://svmrakennus.fi/", "https://svmrakennus.fi/en/"},
		{"/en/", "en", "https://svmrakennus.fi/en/", "https://svmrakennus.fi/"},
		{"/services/renovation/", "fi", "https://svmrakennus.fi/services/renovation/", "https://svmrakennus.fi/en/services/renovation/"},
		{"/en/services/renovation/", "en", "https://svmrakennus.fi/en/services/renovation/", "https://svmrakennus.fi/services/renovation/"},
		{"/privacy/", "fi", "https://svmrakennus.fi/privacy/", "https://svmrakennus.fi/en/privacy/"},
		{"/en/privacy/", "en", "https://svmrakennus.fi/en/privacy/", "https://svmrakennus.fi/privacy/"},
	} {
		w := request(a, "GET", tc.path, nil, nil, "")
		if w.Code != 200 {
			t.Fatalf("%s: expected 200, got %d", tc.path, w.Code)
		}
		body := w.Body.String()
		for _, expected := range []string{
			`<html lang="` + tc.lang + `">`,
			`<link rel="canonical" href="` + tc.canonical + `">`,
			`hreflang="x-default" href="https://svmrakennus.fi/`,
			`hreflang="fi" href="https://svmrakennus.fi/`,
			`hreflang="en" href="https://svmrakennus.fi/en/`,
		} {
			if !strings.Contains(body, expected) {
				t.Errorf("%s: expected HTML fragment %q", tc.path, expected)
			}
		}
		if !strings.Contains(body, `href="`+strings.TrimPrefix(tc.other, s.Settings.BaseURL)+`"`) {
			t.Errorf("%s: language switch does not point to %s", tc.path, tc.other)
		}
	}

	linkRE := regexp.MustCompile(`href="([^"]+)"`)
	for _, page := range []string{
		"/", "/en/", "/services/renovation/", "/en/services/renovation/",
		"/projects/home-renovation/", "/en/projects/home-renovation/",
		"/privacy/", "/en/privacy/", "/cookies/", "/en/cookies/", "/terms/", "/en/terms/",
	} {
		pageHTML := request(a, "GET", page, nil, nil, "").Body.String()
		for _, m := range linkRE.FindAllStringSubmatch(pageHTML, -1) {
			href := html.UnescapeString(m[1])
			u, err := url.Parse(href)
			if err != nil {
				t.Fatalf("%s: malformed href %q: %v", page, href, err)
			}
			if u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "/api/") {
				continue
			}
			result := request(a, "GET", u.Path, nil, nil, "")
			if result.Code != 200 {
				t.Errorf("%s: broken internal link %q (HTTP %d)", page, href, result.Code)
			}
			if strings.HasPrefix(u.Path, "/fi/") {
				t.Errorf("%s: old /fi/ link remains: %q", page, href)
			}
		}
	}

	for old, target := range map[string]string{
		"/fi": "/",
		"/fi/": "/",
		"/fi/services/renovation/": "/services/renovation/",
		"/fi/projects/home-renovation/": "/projects/home-renovation/",
		"/fi/privacy/": "/privacy/",
		"/en": "/en/",
	} {
		w := request(a, "GET", old+"?utm_source=test", nil, nil, "")
		if w.Code != 301 || w.Header().Get("Location") != target+"?utm_source=test" {
			t.Errorf("%s: wanted 301 to %s, got %d to %q", old, target, w.Code, w.Header().Get("Location"))
		}
	}

	sitemap := request(a, "GET", "/sitemap.xml", nil, nil, "").Body.String()
	if !strings.Contains(sitemap, "<loc>https://svmrakennus.fi/</loc>") ||
		!strings.Contains(sitemap, "<loc>https://svmrakennus.fi/en/</loc>") ||
		!strings.Contains(sitemap, "<loc>https://svmrakennus.fi/services/renovation/</loc>") ||
		strings.Contains(sitemap, "/fi/") {
		t.Fatal("sitemap does not contain canonical Finnish and English routes")
	}
}
