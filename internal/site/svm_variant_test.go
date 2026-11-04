package site

import (
	"context"
	"strings"
	"testing"
)

func TestSVMFinnishDefaultAndUkrainianEditor(t *testing.T) {
	a := testApp(t)
	w := request(a, "GET", "/?utm_source=partner", nil, nil, "")
	if w.Code != 302 || w.Header().Get("Location") != "/fi/?utm_source=partner" {
		t.Fatalf("Finnish redirect lost language or campaign: %s", w.Header().Get("Location"))
	}
	s, _, err := a.loadState(context.Background())
	if err != nil || s.Settings.Company != "SVM Rakennus" || s.Settings.MainLanguage != "fi" {
		t.Fatal("wrong SVM defaults")
	}
	if len(s.Text["uk"]) != len(s.Text["fi"]) || s.Services[5].Title["uk"] == "" {
		t.Fatal("missing Ukrainian editorial translations")
	}
	for _, lang := range []string{"fi", "en"} {
		html := request(a, "GET", "/"+lang+"/", nil, nil, "").Body.String()
		if strings.Contains(html, "NORDRA") || strings.Contains(html, "Новий проєкт") || strings.Contains(html, s.Text["uk"]["hero.title"]) {
			t.Fatal("old brand or internal translation appeared in public HTML")
		}
		for _, token := range []string{`class="hero"`, `class="hero-shade"`, `class="text-link light"`, `--accent:#fbbb21`, `--paper:#f8faff`, `--ink:#090b19`, s.Text[lang]["hero.title"]} {
			if !strings.Contains(html, token) {
				t.Fatalf("Contrast layout missing: %s", token)
			}
		}
		if strings.Contains(html, `class="hero-visual"`) || strings.Contains(html, "preview-data") {
			t.Fatal("unselected layout or concept handlers appeared in production HTML")
		}
	}
	html := request(a, "GET", "/admin", nil, nil, "").Body.String()
	if !strings.Contains(html, `<html lang="uk">`) || !strings.Contains(html, "Увійти до кабінету") || !strings.Contains(html, "admin-help.js") {
		t.Fatal("Ukrainian admin missing")
	}
	s.Settings.BaseURL = "https://svm.example.fi"
	putState(t, a, s)
	html = request(a, "GET", "/en/services/renovation/", nil, nil, "").Body.String()
	if !strings.Contains(html, `hreflang="x-default" href="https://svm.example.fi/fi/services/renovation/"`) {
		t.Fatal("default hreflang must point to the matching Finnish page")
	}
}

func TestSVMThemeValidationAndRendering(t *testing.T) {
	a := testApp(t)
	s, _, _ := a.loadState(context.Background())
	s.Settings.Paper = "#faf9f4"
	s.Settings.Ink = "#18372a"
	s.Settings.Surface = "#e9efda"
	s.Settings.HeadingFont = "sans"
	if err := validateState(s); err != nil {
		t.Fatal(err)
	}
	putState(t, a, s)
	html := request(a, "GET", "/fi/", nil, nil, "").Body.String()
	for _, token := range []string{`class="font-sans"`, "--paper:#faf9f4", "--ink:#18372a", "--surface:#e9efda"} {
		if !strings.Contains(html, token) {
			t.Fatalf("theme control missing: %s", token)
		}
	}
	for _, value := range []string{"red", "#fff", "#ffffff;background:url(javascript:x)", "#GGGGGG"} {
		s.Settings.Paper = value
		if validateState(s) == nil {
			t.Fatalf("unsafe theme colour accepted: %s", value)
		}
	}
	s.Settings.Paper = "#faf9f4"
	s.Settings.HeadingFont = "serif;script"
	if validateState(s) == nil {
		t.Fatal("unsafe font choice accepted")
	}
}

func TestSVMContactVisibilityOnDetailPages(t *testing.T) {
	a := testApp(t)
	s, _, _ := a.loadState(context.Background())
	for i := range s.Blocks {
		if s.Blocks[i].ID == "contact" {
			s.Blocks[i].Enabled = false
		}
	}
	putState(t, a, s)
	for _, path := range []string{"/fi/", "/en/", "/fi/services/renovation/", "/en/projects/home-renovation/"} {
		html := request(a, "GET", path, nil, nil, "").Body.String()
		if strings.Contains(html, `id="lead-form"`) || strings.Contains(html, `id="contact"`) {
			t.Fatalf("disabled contact still appears on %s", path)
		}
		if !strings.Contains(html, "mailto:hello@example.com") {
			t.Fatal("contact link must still provide an email fallback")
		}
	}
}

func TestSVMNotificationLanguageAndHelsinkiTime(t *testing.T) {
	a := testApp(t)
	s, _, _ := a.loadState(context.Background())
	subject, body := notificationContent(s, Lead{ID: "test", Created: "2026-07-01T10:05:00Z", Language: "fi", Name: "Example", Service: "firestopping"})
	if subject != "Нове звернення — SVM Rakennus" {
		t.Fatal("notification subject is not Ukrainian")
	}
	for _, token := range []string{"01.07.2026 13:05 EEST", "Palokatkotyöt", "Протипожежна", "Suomi — фінська"} {
		if !strings.Contains(body, token) {
			t.Fatalf("notification missing %s", token)
		}
	}
	if value := helsinkiTime("2026-01-01T10:05:00Z"); value != "01.01.2026 12:05 EET" {
		t.Fatalf("winter time incorrect: %s", value)
	}
	if value := helsinkiTime("invalid"); value != "invalid" {
		t.Fatal("invalid date must remain inspectable")
	}
}
