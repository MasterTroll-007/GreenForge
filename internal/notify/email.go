package notify

import (
	"context"
	"fmt"
	"log"
	"net/smtp"
)

// EmailProvider sends notifications via SMTP.
type EmailProvider struct {
	address  string
	smtpHost string
	smtpPort int
	from     string
	password string
}

func NewEmailProvider(address string) *EmailProvider {
	return &EmailProvider{
		address:  address,
		smtpHost: "smtp.gmail.com",
		smtpPort: 587,
		from:     "greenforge@localhost",
	}
}

func (p *EmailProvider) Name() string { return "email" }

func (p *EmailProvider) Available() bool {
	return p.address != ""
}

func (p *EmailProvider) Send(ctx context.Context, msg Message) error {
	subject := fmt.Sprintf("[GreenForge] %s", msg.Title)
	body := formatEmailBody(msg)

	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		p.from, p.address, subject, body)

	if p.password == "" {
		log.Printf("Email notification (no SMTP configured): To=%s Subject=%s", p.address, subject)
		return nil
	}

	auth := smtp.PlainAuth("", p.from, p.password, p.smtpHost)
	addr := fmt.Sprintf("%s:%d", p.smtpHost, p.smtpPort)
	return smtp.SendMail(addr, auth, p.from, []string{p.address}, []byte(message))
}

func formatEmailBody(msg Message) string {
	body := msg.Title + "\n"
	body += "=" + repeatStr("=", len(msg.Title)) + "\n\n"

	if msg.Project != "" {
		body += fmt.Sprintf("Project: %s\n", msg.Project)
	}
	body += fmt.Sprintf("Severity: %s\n", msg.Severity)
	body += fmt.Sprintf("Event: %s\n\n", msg.Event)
	body += msg.Body + "\n"

	if len(msg.Actions) > 0 {
		body += "\nActions:\n"
		for _, a := range msg.Actions {
			body += fmt.Sprintf("  - %s: %s\n", a.Label, a.Command)
		}
	}

	body += "\n--\nGreenForge AI Developer Agent"
	return body
}

func repeatStr(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
