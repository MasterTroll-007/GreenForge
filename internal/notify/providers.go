package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// WhatsAppProvider sends notifications via WhatsApp Business API.
// Supports two modes:
// 1. WhatsApp Business Cloud API (Meta) - requires access token + phone number ID
// 2. Custom webhook (e.g. Baileys-based gateway) - POST JSON to a URL
type WhatsAppProvider struct {
	phone       string
	accessToken string // Meta Cloud API access token
	phoneNumID  string // Meta Cloud API phone number ID
	webhookURL  string // Alternative: custom webhook URL
	client      *http.Client
}

// WhatsAppConfig holds WhatsApp provider configuration.
type WhatsAppConfig struct {
	Phone       string
	AccessToken string // For Meta Cloud API
	PhoneNumID  string // For Meta Cloud API
	WebhookURL  string // For custom gateway (Baileys etc.)
}

func NewWhatsAppProvider(phone string) *WhatsAppProvider {
	return &WhatsAppProvider{
		phone:  phone,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// NewWhatsAppProviderWithConfig creates a fully configured WhatsApp provider.
func NewWhatsAppProviderWithConfig(cfg WhatsAppConfig) *WhatsAppProvider {
	return &WhatsAppProvider{
		phone:       cfg.Phone,
		accessToken: cfg.AccessToken,
		phoneNumID:  cfg.PhoneNumID,
		webhookURL:  cfg.WebhookURL,
		client:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *WhatsAppProvider) Name() string    { return "whatsapp" }
func (p *WhatsAppProvider) Available() bool { return p.phone != "" && (p.accessToken != "" || p.webhookURL != "") }

func (p *WhatsAppProvider) Send(ctx context.Context, msg Message) error {
	text := formatWhatsAppMessage(msg)

	if p.webhookURL != "" {
		return p.sendViaWebhook(ctx, text)
	}
	if p.accessToken != "" {
		return p.sendViaCloudAPI(ctx, text)
	}

	// Fallback: log only
	log.Printf("WhatsApp notification to %s: %s", p.phone, msg.Title)
	return nil
}

func (p *WhatsAppProvider) sendViaCloudAPI(ctx context.Context, text string) error {
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", p.phoneNumID)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                p.phone,
		"type":              "text",
		"text":              map[string]string{"body": text},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.accessToken)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp cloud API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("whatsapp cloud API error: status %d", resp.StatusCode)
	}
	return nil
}

func (p *WhatsAppProvider) sendViaWebhook(ctx context.Context, text string) error {
	payload := map[string]string{
		"phone":   p.phone,
		"message": text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("whatsapp webhook error: status %d", resp.StatusCode)
	}
	return nil
}

func formatWhatsAppMessage(msg Message) string {
	icon := "ℹ️"
	switch msg.Severity {
	case "warning":
		icon = "⚠️"
	case "error":
		icon = "🔴"
	case "critical":
		icon = "🚨"
	}

	text := fmt.Sprintf("%s *%s*\n", icon, msg.Title)
	if msg.Project != "" {
		text += fmt.Sprintf("📦 Project: %s\n", msg.Project)
	}
	text += "\n" + msg.Body

	if len(msg.Actions) > 0 {
		text += "\n\n"
		for _, a := range msg.Actions {
			text += fmt.Sprintf("→ %s: %s\n", a.Label, a.Command)
		}
	}

	return text
}

// ---

// SMSProvider sends notifications via Twilio REST API.
type SMSProvider struct {
	phone      string
	accountSID string
	authToken  string
	fromNumber string
	client     *http.Client
}

// SMSConfig holds Twilio configuration.
type SMSConfig struct {
	Phone      string // recipient phone
	AccountSID string
	AuthToken  string
	FromNumber string // Twilio phone number
}

func NewSMSProvider(phone string) *SMSProvider {
	return &SMSProvider{
		phone:  phone,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// NewSMSProviderWithConfig creates a fully configured SMS/Twilio provider.
func NewSMSProviderWithConfig(cfg SMSConfig) *SMSProvider {
	return &SMSProvider{
		phone:      cfg.Phone,
		accountSID: cfg.AccountSID,
		authToken:  cfg.AuthToken,
		fromNumber: cfg.FromNumber,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *SMSProvider) Name() string    { return "sms" }
func (p *SMSProvider) Available() bool { return p.phone != "" && p.accountSID != "" }

func (p *SMSProvider) Send(ctx context.Context, msg Message) error {
	if p.accountSID == "" || p.authToken == "" {
		log.Printf("SMS notification to %s: %s (no Twilio credentials)", p.phone, msg.Title)
		return nil
	}

	// Twilio SMS API
	text := formatSMSMessage(msg)

	url := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", p.accountSID)

	data := fmt.Sprintf("To=%s&From=%s&Body=%s", p.phone, p.fromNumber, text)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(p.accountSID, p.authToken)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("twilio SMS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("twilio SMS error: status %d", resp.StatusCode)
	}
	return nil
}

func formatSMSMessage(msg Message) string {
	// SMS has 160 char limit, be concise
	text := fmt.Sprintf("[GreenForge] %s", msg.Title)
	if msg.Project != "" {
		text += fmt.Sprintf(" (%s)", msg.Project)
	}
	if len(text) > 155 {
		text = text[:155] + "..."
	}
	return text
}

// ---

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
