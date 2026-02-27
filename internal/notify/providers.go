package notify

import (
	"context"
	"fmt"
	"log"
)

// WhatsAppProvider sends notifications via WhatsApp Business API.
type WhatsAppProvider struct {
	phone string
}

func NewWhatsAppProvider(phone string) *WhatsAppProvider {
	return &WhatsAppProvider{phone: phone}
}

func (p *WhatsAppProvider) Name() string    { return "whatsapp" }
func (p *WhatsAppProvider) Available() bool { return p.phone != "" }

func (p *WhatsAppProvider) Send(ctx context.Context, msg Message) error {
	// TODO: Implement WhatsApp Business API / Baileys integration
	log.Printf("WhatsApp notification to %s: %s", p.phone, msg.Title)
	return nil
}

// SMSProvider sends notifications via Twilio or custom SMS gateway.
type SMSProvider struct {
	phone string
}

func NewSMSProvider(phone string) *SMSProvider {
	return &SMSProvider{phone: phone}
}

func (p *SMSProvider) Name() string    { return "sms" }
func (p *SMSProvider) Available() bool { return p.phone != "" }

func (p *SMSProvider) Send(ctx context.Context, msg Message) error {
	// TODO: Implement Twilio SMS
	log.Printf("SMS notification to %s: %s", p.phone, msg.Title)
	return nil
}

// CLIProvider shows notifications as CLI toast/log messages.
type CLIProvider struct{}

func NewCLIProvider() *CLIProvider {
	return &CLIProvider{}
}

func (p *CLIProvider) Name() string    { return "cli" }
func (p *CLIProvider) Available() bool { return true }

func (p *CLIProvider) Send(ctx context.Context, msg Message) error {
	icon := "ℹ"
	switch msg.Severity {
	case "warning":
		icon = "⚠"
	case "error":
		icon = "✗"
	case "critical":
		icon = "🚨"
	}

	fmt.Printf("\n%s [%s] %s\n", icon, msg.Project, msg.Title)
	if msg.Body != "" {
		fmt.Printf("  %s\n", msg.Body)
	}
	return nil
}
