package site

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

func (a *App) sendNotification(ctx context.Context, s State, l Lead, confirmation bool) error {
	host := os.Getenv("SMTP_HOST")
	user := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	if host == "" || from == "" {
		return fmt.Errorf("SMTP is not configured; lead remains available in the admin panel")
	}
	fa, e := mail.ParseAddress(from)
	if e != nil || strings.ContainsAny(from, "\r\n") {
		return fmt.Errorf("invalid SMTP_FROM")
	}
	to := s.Settings.NotificationEmail
	if to == "" {
		to = os.Getenv("MAIL_TO")
	}
	if to == "" {
		to = s.Settings.Email
	}
	if confirmation {
		to = l.Email
	}
	if !validEmail(to) {
		return fmt.Errorf("valid notification email is required")
	}
	subject, body := notificationContent(s, l)
	for _, m := range l.Files {
		body += "\nПриватне вкладення (потрібен вхід до кабінету): " + s.Settings.BaseURL + m.URL
	}
	if confirmation {
		subject = s.Text[l.Language]["confirmation.subject"]
		body = s.Text[l.Language]["confirmation.body"] + "\n\n" + s.Settings.Company + "\n" + s.Settings.Phone + "\n" + s.Settings.Email
	}
	subject = "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(cleanLine(subject))) + "?="
	headers := "From: " + cleanLine(from) + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\nMessage-ID: <" + randomID() + "@" + cleanLine(host) + ">\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n"
	if !confirmation && validEmail(l.Email) {
		headers += "Reply-To: " + l.Email + "\r\n"
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var wrapped strings.Builder
	for len(encoded) > 76 {
		wrapped.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	wrapped.WriteString(encoded + "\r\n")
	p, e := strconv.Atoi(port)
	if e != nil || (p != 465 && p != 587 && p != 2525) {
		return fmt.Errorf("SMTP_PORT must be 465, 587 or 2525")
	}
	address := net.JoinHostPort(host, port)
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, e := dialer.DialContext(ctx, "tcp", address)
	if e != nil {
		return fmt.Errorf("SMTP connection failed")
	}
	defer conn.Close()
	deadline := time.Now().Add(25 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if p == 465 {
		tc := tls.Client(conn, config)
		if e = tc.HandshakeContext(ctx); e != nil {
			return fmt.Errorf("SMTP TLS handshake failed")
		}
		conn = tc
	}
	client, e := smtp.NewClient(conn, host)
	if e != nil {
		return fmt.Errorf("SMTP greeting failed")
	}
	defer client.Close()
	if p != 465 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server must support STARTTLS")
		}
		if e = client.StartTLS(config); e != nil {
			return fmt.Errorf("SMTP TLS failed")
		}
	}
	if user != "" {
		if e = client.Auth(smtp.PlainAuth("", user, password, host)); e != nil {
			return fmt.Errorf("SMTP authentication failed")
		}
	}
	if e = client.Mail(fa.Address); e != nil {
		return fmt.Errorf("SMTP sender rejected")
	}
	if e = client.Rcpt(to); e != nil {
		return fmt.Errorf("SMTP recipient rejected")
	}
	writer, e := client.Data()
	if e != nil {
		return fmt.Errorf("SMTP DATA failed")
	}
	if _, e = writer.Write([]byte(headers + "\r\n" + wrapped.String())); e != nil {
		return fmt.Errorf("SMTP body failed")
	}
	if e = writer.Close(); e != nil {
		return fmt.Errorf("SMTP message rejected")
	}
	_ = client.Quit()
	return nil
}

func helsinkiTime(value string) string {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	zone, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		return value
	}
	return t.In(zone).Format("02.01.2006 15:04 MST")
}

func notificationContent(s State, l Lead) (string, string) {
	service := l.Service
	for _, item := range s.Services {
		if item.ID == l.Service {
			service = item.Title.Get("fi")
			if item.Title["uk"] != "" {
				service += " — " + item.Title["uk"]
			}
			break
		}
	}
	language := "English — англійська"
	if l.Language == "fi" {
		language = "Suomi — фінська"
	}
	return "Нове звернення — " + s.Settings.Company, fmt.Sprintf("ID: %s\nЧас (Гельсінкі): %s\nUTC: %s\nМова: %s\nІм’я: %s\nКомпанія: %s\nEmail: %s\nТелефон: %s\nПослуга: %s\n\n%s\n\nКабінет: %s/admin\n", l.ID, helsinkiTime(l.Created), l.Created, language, l.Name, l.Company, l.Email, l.Phone, service, l.Message, s.Settings.BaseURL)
}
