package site

import (
    "fmt"
    "html"
    "net/url"
    "path"
    "sort"
    "strings"
)

// Email markup is assembled with escaped user data and allowlisted theme values.
// Inline styles and table layout work in Gmail, Outlook and most mobile clients.
func brandedEmailHTML(s State, l Lead, customer bool) string {
    esc := html.EscapeString
    color := func(value, fallback string) string {
        if colorRE.MatchString(value) {
            return value
        }
        return fallback
    }
    accent := color(s.Settings.Accent, "#36594b")
    paper := color(s.Settings.Paper, "#f6f4ee")
    ink := color(s.Settings.Ink, "#243a30")
    surface := color(s.Settings.Surface, "#e8ede7")
    headingFont := "Georgia, 'Times New Roman', serif"
    if s.Settings.HeadingFont == "sans" {
        headingFont = "Arial, Helvetica, sans-serif"
    }
    company := strings.TrimSpace(s.Settings.Company)
    if company == "" {
        company = "SVM Rakennus"
    }
    home := "https://svmrakennus.fi"
    if u, err := url.Parse(s.Settings.BaseURL); err == nil && u.Scheme == "https" &&
        u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" {
        home = u.String()
    }
    headerBrand := "<span style=\"display:inline-block;font-weight:800;letter-spacing:-1px;border-radius:6px;padding:9px 12px;background:" + accent + ";color:#ffffff\">SVM</span> <span style=\"font-weight:700;color:" + ink + "\">" + esc(company) + "</span>"
    if safeFileRef(s.Settings.Logo) && strings.HasPrefix(s.Settings.Logo, "/static/images/") {
        ext := strings.ToLower(path.Ext(s.Settings.Logo))
        if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" || ext == ".gif" {
            logoURL := home + s.Settings.Logo
            headerBrand = "<img src=\"" + esc(logoURL) + "\" alt=\"" + esc(company) + "\" style=\"max-width:160px;max-height:56px;vertical-align:middle;border:0\"> <span style=\"margin-left:12px;font-weight:700;color:" + ink + "\">" + esc(company) + "</span>"
        }
    }
    isFI := l.Language == "fi"
    label, heading, introduction, footer, button, buttonURL := "", "", "", "", "", home
    subjectLine := ""
    if customer {
        if isFI {
            label = "VIESTISI ON VASTAANOTETTU"
            heading = "Kiitos yhteydenotostasi!"
            introduction = "Olemme vastaanottaneet viestisi. Otamme sinuun pian yhteyttä."
            footer = "Tämä on automaattinen vastaanottovahvistus, ei tarjous tai sitova tilaus."
            button = "Siirry verkkosivulle"
            subjectLine = "Kiitos yhteydenotostasi"
        } else {
            label = "ENQUIRY RECEIVED"
            heading = "Thank you for your enquiry!"
            introduction = "We have received your message. We will contact you shortly."
            footer = "This is an automated acknowledgement, not an offer or binding order."
            button = "Visit our website"
            subjectLine = "Thank you for your enquiry"
        }
    } else {
        label = "NEW WEBSITE ENQUIRY"
        heading = "New project enquiry"
        introduction = "A new customer enquiry has been saved to the SVM Rakennus administration panel."
        footer = "Customer details and private attachments are accessible through the administration panel."
        button = "Open administration"
        buttonURL = home + "/admin"
        subjectLine = "New enquiry received"
    }

    var b strings.Builder
    b.Grow(4200 + len(l.Message))
    b.WriteString("<!doctype html><html lang=\"")
    if isFI && customer {
        b.WriteString("fi")
    } else {
        b.WriteString("en")
    }
    b.WriteString("\"><head><meta charset=\"UTF-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>")
    b.WriteString(esc(subjectLine))
    b.WriteString("</title></head><body style=\"margin:0;padding:0;background:")
    b.WriteString(paper)
    b.WriteString(";font-family:Arial,Helvetica,sans-serif;color:")
    b.WriteString(ink)
    b.WriteString(";\">")
    b.WriteString("<table role=\"presentation\" cellpadding=\"0\" cellspacing=\"0\" border=\"0\" width=\"100%\" style=\"background:")
    b.WriteString(paper)
    b.WriteString(";\"><tr><td align=\"center\" style=\"padding:32px 14px\">")
    b.WriteString("<table role=\"presentation\" cellpadding=\"0\" cellspacing=\"0\" border=\"0\" width=\"100%\" style=\"max-width:640px;background:#ffffff;border-collapse:collapse;border:1px solid ")
    b.WriteString(surface)
    b.WriteString(";border-top:6px solid ")
    b.WriteString(accent)
    b.WriteString(";\">")
    b.WriteString("<tr><td style=\"padding:30px 36px 22px;border-bottom:1px solid ")
    b.WriteString(surface)
    b.WriteString(";font-size:18px;line-height:28px\">")
    b.WriteString(headerBrand)
    b.WriteString("</td></tr><tr><td style=\"padding:34px 36px 12px\">")
    b.WriteString("<div style=\"color:")
    b.WriteString(accent)
    b.WriteString(";font-size:11px;font-weight:700;letter-spacing:1.6px;text-transform:uppercase\">")
    b.WriteString(esc(label))
    b.WriteString("</div><h1 style=\"font-family:")
    b.WriteString(headingFont)
    b.WriteString(";font-size:30px;line-height:1.2;letter-spacing:-0.5px;font-weight:700;margin:15px 0 14px;color:")
    b.WriteString(ink)
    b.WriteString("\">")
    b.WriteString(esc(heading))
    b.WriteString("</h1><p style=\"font-size:16px;line-height:1.65;margin:0 0 22px;color:")
    b.WriteString(ink)
    b.WriteString("\">")
    b.WriteString(esc(introduction))
    b.WriteString("</p></td></tr>")
    b.WriteString("<tr><td style=\"padding:0 36px 24px\"><table role=\"presentation\" cellpadding=\"0\" cellspacing=\"0\" border=\"0\" width=\"100%\" style=\"background:")
    b.WriteString(surface)
    b.WriteString(";border-radius:4px\">")
    row := func(title, value string) {
        if strings.TrimSpace(value) == "" {
            return
        }
        b.WriteString("<tr><td style=\"padding:11px 18px 4px;font-size:11px;letter-spacing:0.4px;color:")
        b.WriteString(ink)
        b.WriteString(";opacity:0.8;font-weight:700\">")
        b.WriteString(esc(title))
        b.WriteString("</td></tr><tr><td style=\"padding:0 18px 12px;font-size:14px;line-height:1.55;color:")
        b.WriteString(ink)
        b.WriteString(";overflow-wrap:anywhere;word-break:break-word\">")
        b.WriteString(strings.ReplaceAll(esc(value), "\n", "<br>"))
        b.WriteString("</td></tr>")
    }
    if customer {
        if isFI {
            row("Nimi", l.Name)
            row("Viite", l.ID)
        } else {
            row("Name", l.Name)
            row("Reference", l.ID)
        }
    } else {
        service := emailServiceName(s, l.Service, "fi")
        row("Reference ID", l.ID)
        row("Date · Helsinki", helsinkiTime(l.Created))
        row("Name", l.Name)
        row("Company", l.Company)
        row("Phone", l.Phone)
        row("Email", l.Email)
        row("Requested service", service)
        row("Customer language", l.Language)
        row("Message", l.Message)
        row("Landing page / source", l.Source)
        if len(l.Campaign) > 0 {
            keys := make([]string, 0, len(l.Campaign))
            for k := range l.Campaign {
                keys = append(keys, k)
            }
            sort.Strings(keys)
            for _, k := range keys {
                row(k, l.Campaign[k])
            }
        }
        if len(l.Files) > 0 {
            names := make([]string, 0, len(l.Files))
            for _, m := range l.Files {
                names = append(names, m.Name)
            }
            row(fmt.Sprintf("Attachments (%d) — available in admin", len(names)), strings.Join(names, "\n"))
        }
    }
    b.WriteString("</table></td></tr><tr><td style=\"padding:0 36px 30px\">")
    b.WriteString("<a href=\"")
    b.WriteString(esc(buttonURL))
    b.WriteString("\" style=\"display:inline-block;padding:14px 23px;background:")
    b.WriteString(accent)
    b.WriteString(";color:#ffffff;text-decoration:none;font-size:14px;font-weight:700;border-radius:4px\">")
    b.WriteString(esc(button))
    b.WriteString("</a></td></tr><tr><td style=\"padding:24px 36px;background:")
    b.WriteString(surface)
    b.WriteString(";font-size:12px;line-height:1.6;color:")
    b.WriteString(ink)
    b.WriteString("\"><strong>")
    b.WriteString(esc(company))
    b.WriteString("</strong><br>")
    b.WriteString(esc(footer))
    b.WriteString("<br><a style=\"color:")
    b.WriteString(accent)
    b.WriteString(";text-decoration:underline\" href=\"mailto:")
    b.WriteString(systemSenderEmail)
    b.WriteString("\">")
    b.WriteString(systemSenderEmail)
    b.WriteString("</a></td></tr></table></td></tr></table></body></html>")
    return b.String()
}

func emailServiceName(s State, id, lang string) string {
    for _, service := range s.Services {
        if service.ID == id {
            return service.Title.Get(lang)
        }
    }
    return id
}

func emailPlainBody(s State, l Lead, customer bool) (string, string) {
    if customer {
        if l.Language == "fi" {
            return "Kiitos yhteydenotostasi — " + s.Settings.Company,
                "Hei " + l.Name + ",\n\nOlemme vastaanottaneet viestisi. Otamme sinuun pian yhteyttä.\n\n" +
                    "Viitenumero: " + l.ID + "\n\nTämä on automaattinen vastaanottovahvistus, ei tarjous tai sitova tilaus.\n\n" + s.Settings.Company + "\n" + systemSenderEmail
        }
        return "Thank you for your enquiry — " + s.Settings.Company,
            "Hello " + l.Name + ",\n\nWe have received your message. We will contact you shortly.\n\n" +
                "Reference: " + l.ID + "\n\nThis is an automated acknowledgement, not an offer or binding order.\n\n" + s.Settings.Company + "\n" + systemSenderEmail
    }
    subject, oldBody := notificationContent(s, l)
    return subject, oldBody + "\nSource: " + l.Source + "\n\n" +
        "Attachments are available in the secure admin panel.\n"
}
