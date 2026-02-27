package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
)

// EmailProvider sends notifications via SMTP.
type EmailProvider struct {
	to       string
	smtpHost string
	smtpPort int
	from     string
	username string
	password string
	useTLS   bool
}

// EmailConfig holds SMTP configuration.
type EmailConfig struct {
	To       string
	From     string
	SMTPHost string
	SMTPPort int
	Username string
	Password string
	UseTLS   bool
}

func NewEmailProvider(address string) *EmailProvider {
	return &EmailProvider{
		to:       address,
		smtpHost: "smtp.gmail.com",
		smtpPort: 587,
		from:     "greenforge@localhost",
		useTLS:   true,
	}
}

// NewEmailProviderWithConfig creates a fully configured email provider.
func NewEmailProviderWithConfig(cfg EmailConfig) *EmailProvider {
	host := cfg.SMTPHost
	if host == "" {
		host = "smtp.gmail.com"
	}
	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	from := cfg.From
	if from == "" {
		from = cfg.Username
	}

	return &EmailProvider{
		to:       cfg.To,
		smtpHost: host,
		smtpPort: port,
		from:     from,
		username: cfg.Username,
		password: cfg.Password,
		useTLS:   cfg.UseTLS,
	}
}

func (p *EmailProvider) Name() string { return "email" }

func (p *EmailProvider) Available() bool {
	return p.to != ""
}

func (p *EmailProvider) Send(ctx context.Context, msg Message) error {
	subject := fmt.Sprintf("[GreenForge] %s", msg.Title)

	// Build MIME message with both plain text and HTML
	boundary := "GreenForgeBoundary"
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("From: %s\r\n", p.from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", p.to))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n\r\n", boundary))

	// Plain text part
	sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	sb.WriteString(formatEmailPlainText(msg))
	sb.WriteString("\r\n")

	// HTML part
	sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	sb.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
	sb.WriteString(formatEmailHTML(msg))
	sb.WriteString("\r\n")

	sb.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	message := sb.String()

	// If no SMTP credentials, log instead of sending
	if p.password == "" {
		log.Printf("Email notification (no SMTP password): To=%s Subject=%s", p.to, subject)
		return nil
	}

	addr := fmt.Sprintf("%s:%d", p.smtpHost, p.smtpPort)

	if p.smtpPort == 465 {
		// Implicit TLS (SMTPS)
		return p.sendTLS(addr, message)
	}

	// STARTTLS (port 587 or 25)
	return p.sendSTARTTLS(addr, message)
}

func (p *EmailProvider) sendSTARTTLS(addr, message string) error {
	auth := smtp.PlainAuth("", p.username, p.password, p.smtpHost)

	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer c.Close()

	if p.useTLS {
		tlsCfg := &tls.Config{ServerName: p.smtpHost}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}

	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}

	if err := c.Mail(p.from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}

	if err := c.Rcpt(p.to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	if _, err := w.Write([]byte(message)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}

	return c.Quit()
}

func (p *EmailProvider) sendTLS(addr, message string) error {
	tlsCfg := &tls.Config{ServerName: p.smtpHost}

	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}

	host, _, _ := net.SplitHostPort(addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	auth := smtp.PlainAuth("", p.username, p.password, p.smtpHost)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}

	if err := c.Mail(p.from); err != nil {
		return err
	}
	if err := c.Rcpt(p.to); err != nil {
		return err
	}

	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(message)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	return c.Quit()
}

func formatEmailPlainText(msg Message) string {
	var sb strings.Builder
	sb.WriteString(msg.Title + "\n")
	sb.WriteString(strings.Repeat("=", len(msg.Title)) + "\n\n")

	if msg.Project != "" {
		sb.WriteString(fmt.Sprintf("Project: %s\n", msg.Project))
	}
	sb.WriteString(fmt.Sprintf("Severity: %s\n", msg.Severity))
	sb.WriteString(fmt.Sprintf("Event: %s\n\n", msg.Event))
	sb.WriteString(msg.Body + "\n")

	if len(msg.Actions) > 0 {
		sb.WriteString("\nActions:\n")
		for _, a := range msg.Actions {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", a.Label, a.Command))
		}
	}

	sb.WriteString("\n--\nGreenForge AI Developer Agent")
	return sb.String()
}

func formatEmailHTML(msg Message) string {
	severityColor := "#2ea44f"
	switch msg.Severity {
	case "warning":
		severityColor = "#dbab09"
	case "error":
		severityColor = "#d73a49"
	case "critical":
		severityColor = "#cb2431"
	}

	var sb strings.Builder
	sb.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,sans-serif;max-width:600px;margin:0 auto;padding:20px">`)
	sb.WriteString(fmt.Sprintf(`<div style="border-left:4px solid %s;padding-left:16px">`, severityColor))
	sb.WriteString(fmt.Sprintf(`<h2 style="margin:0 0 8px">%s</h2>`, msg.Title))

	if msg.Project != "" {
		sb.WriteString(fmt.Sprintf(`<p style="color:#888;margin:4px 0">Project: <strong>%s</strong></p>`, msg.Project))
	}

	sb.WriteString(`</div>`)
	sb.WriteString(fmt.Sprintf(`<pre style="background:#1a1a2e;color:#e0e0e0;padding:16px;border-radius:8px;overflow-x:auto;margin:16px 0">%s</pre>`, msg.Body))

	if len(msg.Actions) > 0 {
		sb.WriteString(`<div style="margin-top:16px">`)
		for _, a := range msg.Actions {
			if strings.HasPrefix(a.Command, "http") {
				sb.WriteString(fmt.Sprintf(`<a href="%s" style="display:inline-block;padding:8px 16px;background:#2ea44f;color:white;text-decoration:none;border-radius:6px;margin-right:8px">%s</a>`, a.Command, a.Label))
			} else {
				sb.WriteString(fmt.Sprintf(`<code style="background:#333;padding:4px 8px;border-radius:4px">%s</code> `, a.Command))
			}
		}
		sb.WriteString(`</div>`)
	}

	sb.WriteString(`<hr style="border:none;border-top:1px solid #333;margin:24px 0">`)
	sb.WriteString(`<p style="color:#666;font-size:12px">GreenForge AI Developer Agent</p>`)
	sb.WriteString(`</div>`)

	return sb.String()
}
