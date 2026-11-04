package site

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

var editorPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("article", "section", "header", "footer", "div", "span", "nav", "p", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6", "strong", "em", "b", "i", "u", "ul", "ol", "li", "blockquote", "figure", "figcaption", "a", "img", "table", "thead", "tbody", "tr", "td", "th", "details", "summary")
	p.AllowAttrs("class").Matching(regexp.MustCompile("^[a-zA-Z0-9 _-]{1,200}$")).Globally()
	p.AllowStyles("color", "background-color", "font-size", "font-weight", "font-style", "text-align", "line-height", "padding", "margin", "border-radius", "max-width").Globally()
	p.AllowAttrs("id").Matching(regexp.MustCompile("^section-[a-z0-9-]{1,60}$")).Globally()
	p.AllowAttrs("href").OnElements("a")
	p.AllowRelativeURLs(true)
	p.AllowURLSchemes("https", "mailto", "tel")
	p.AllowAttrs("src").Matching(regexp.MustCompile("^/(static/images/[a-zA-Z0-9_-]+[.](webp|png|jpg|jpeg|gif|svg)|media/[a-f0-9]{48})$")).OnElements("img")
	p.AllowAttrs("alt").OnElements("img")
	p.AllowAttrs("title").OnElements("a", "img")
	return p
}()

func editorHTML(s State, key string) template.HTML {
	return template.HTML(editorPolicy.Sanitize(s.PageHTML[key]))
}

var layoutKey = regexp.MustCompile(`^/(fi|en)/((privacy|cookies|terms|thank-you|@header|@footer)/|(projects|services)/[a-z0-9-]+/)?$`)

func validateEditor(s State) error {
	for key, value := range s.PageHTML {
		if !layoutKey.MatchString(key) || len(value) > 100000 {
			return fmt.Errorf("invalid page or HTML over 100 KB")
		}
	}
	for _, asset := range s.DeletedAssets {
		if !builtInAsset(asset) {
			return fmt.Errorf("invalid deleted asset")
		}
	}
	return nil
}

func builtInAsset(path string) bool {
	switch path {
	case "/static/images/hero.webp", "/static/images/craft.webp", "/static/images/terrace.webp", "/static/images/interior.webp", "/static/images/favicon.svg", "/static/images/svm-logo.svg":
		return true
	}
	return false
}

func (s State) deletedAsset(path string) bool {
	for _, v := range s.DeletedAssets {
		if v == path {
			return true
		}
	}
	return false
}

// Commit reference removal and catalogue removal in the same versioned write.
func (a *App) removeBuiltIn(w http.ResponseWriter, r *http.Request, path string) {
	s, version, err := a.loadState(r.Context())
	if err != nil {
		fail(w, 503, "Storage unavailable")
		return
	}
	if !builtInAsset(path) {
		fail(w, 400, "Unknown built-in asset")
		return
	}
	for key, html := range s.PageHTML {
		if strings.Contains(editorPolicy.Sanitize(html), path) {
			fail(w, 409, "Remove the image from the HTML editor first: "+key)
			return
		}
	}
	clear := func(field *string) {
		if *field == path {
			*field = ""
		}
	}
	for _, field := range []*string{&s.Settings.Logo, &s.Settings.Favicon, &s.Settings.HeroImage, &s.Settings.AboutImage} {
		clear(field)
	}
	clearSEO := func(items map[string]SEO) {
		for k, v := range items {
			clear(&v.Image)
			items[k] = v
		}
	}
	clearSEO(s.SEO)
	for _, list := range [][]Item{s.Services, s.Projects} {
		for i := range list {
			item := &list[i]
			clear(&item.Image)
			clearSEO(item.SEO)
			kept := item.Gallery[:0]
			for _, v := range item.Gallery {
				if v != path {
					kept = append(kept, v)
				}
			}
			item.Gallery = kept
		}
	}
	for i := range s.Documents {
		if s.Documents[i].URL == path {
			s.Documents[i].Enabled = false
			s.Documents[i].URL = ""
		}
	}
	if !s.deletedAsset(path) {
		s.DeletedAssets = append(s.DeletedAssets, path)
	}
	s.Revision++
	s.Updated = now()
	raw, _ := json.Marshal(s)
	if _, err = a.store.Put(r.Context(), "state/settings", raw, version); err != nil {
		fail(w, 409, "Could not delete file. Reload and retry.")
		return
	}
	s.Auth = Auth{}
	jsonOut(w, 200, s)
}
