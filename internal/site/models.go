package site

import "time"

type Bilingual map[string]string

func (b Bilingual) Get(lang string) string {
	if s := b[lang]; s != "" {
		return s
	}
	return b["en"]
}

type SEO struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
}
type Settings struct {
	Company           string `json:"company"`
	LegalName         string `json:"legalName"`
	BusinessID        string `json:"businessId"`
	ContactName       string `json:"contactName"`
	Phone             string `json:"phone"`
	Email             string `json:"email"`
	Address           string `json:"address"`
	Region            string `json:"region"`
	Country           string `json:"country"`
	BaseURL           string `json:"baseUrl"`
	MainLanguage      string `json:"mainLanguage"`
	Logo              string `json:"logo"`
	Favicon           string `json:"favicon"`
	HeroImage         string `json:"heroImage"`
	AboutImage        string `json:"aboutImage"`
	Accent            string `json:"accent"`
	Paper             string `json:"paper"`
	Ink               string `json:"ink"`
	Surface           string `json:"surface"`
	HeadingFont       string `json:"headingFont"`
	Indexing          bool   `json:"indexing"`
	Demo              bool   `json:"demo"`
	Robots            string `json:"robots"`
	NotificationEmail string `json:"notificationEmail"`
	AllowAttachments  bool   `json:"allowAttachments"`
	ReplyConfirmation bool   `json:"replyConfirmation"`
	LeadRetentionDays int    `json:"leadRetentionDays"`
	LinkedIn          string `json:"linkedIn"`
	Instagram         string `json:"instagram"`
	Facebook          string `json:"facebook"`
}
type Item struct {
	ID       string         `json:"id"`
	Slug     string         `json:"slug"`
	Enabled  bool           `json:"enabled"`
	Demo     bool           `json:"demo"`
	Title    Bilingual      `json:"title"`
	Summary  Bilingual      `json:"summary"`
	Body     Bilingual      `json:"body"`
	Category Bilingual      `json:"category"`
	Location string         `json:"location"`
	Image    string         `json:"image"`
	Gallery  []string       `json:"gallery"`
	Area     string         `json:"area"`
	Year     string         `json:"year"`
	SEO      map[string]SEO `json:"seo"`
}
type FAQ struct {
	Question Bilingual `json:"question"`
	Answer   Bilingual `json:"answer"`
}
type Review struct {
	Name    string    `json:"name"`
	Company string    `json:"company"`
	Text    Bilingual `json:"text"`
	Enabled bool      `json:"enabled"`
}
type Document struct {
	Title       Bilingual `json:"title"`
	Description Bilingual `json:"description"`
	URL         string    `json:"url"`
	Enabled     bool      `json:"enabled"`
}
type Block struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
type EventRule struct {
	Enabled  bool   `json:"enabled"`
	GA       string `json:"ga"`
	AdsLabel string `json:"adsLabel"`
	Meta     string `json:"meta"`
	LinkedIn string `json:"linkedIn"`
	TikTok   string `json:"tikTok"`
}
type Tracking struct {
	Mode     string               `json:"mode"`
	GA4      string               `json:"ga4"`
	Ads      string               `json:"ads"`
	GTM      string               `json:"gtm"`
	Meta     string               `json:"meta"`
	LinkedIn string               `json:"linkedIn"`
	TikTok   string               `json:"tikTok"`
	Events   map[string]EventRule `json:"events"`
}
type Auth struct {
	Hash    string `json:"hash"`
	Version int    `json:"version"`
	Changed bool   `json:"changed"`
}
type State struct {
	Schema    int                          `json:"schema"`
	Revision  int                          `json:"revision"`
	Updated   string                       `json:"updated"`
	Settings  Settings                     `json:"settings"`
	Text      map[string]map[string]string `json:"text"`
	Blocks    []Block                      `json:"blocks"`
	Services  []Item                       `json:"services"`
	Projects  []Item                       `json:"projects"`
	FAQs      []FAQ                        `json:"faqs"`
	Reviews   []Review                     `json:"reviews"`
	Documents []Document                   `json:"documents"`
	SEO       map[string]SEO               `json:"seo"`
	Tracking  Tracking                     `json:"tracking"`
	Redirects []Redirect                   `json:"redirects"`
	Auth      Auth                         `json:"auth"`
}

type Redirect struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type Media struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	Private bool   `json:"private"`
	Created string `json:"created"`
	URL     string `json:"url"`
}
type Lead struct {
	ID               string            `json:"id"`
	Created          string            `json:"created"`
	Language         string            `json:"language"`
	Name             string            `json:"name"`
	Company          string            `json:"company"`
	Email            string            `json:"email"`
	Phone            string            `json:"phone"`
	Service          string            `json:"service"`
	Message          string            `json:"message"`
	Status           string            `json:"status"`
	Notes            string            `json:"notes"`
	Source           string            `json:"source"`
	Campaign         map[string]string `json:"campaign"`
	Files            []Media           `json:"files"`
	Consent          bool              `json:"consent"`
	EmailStatus      string            `json:"emailStatus"`
	EmailAttempts    int               `json:"emailAttempts"`
	EmailLastAttempt string            `json:"emailLastAttempt"`
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }
