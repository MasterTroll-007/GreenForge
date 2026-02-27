package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TelegramProvider sends notifications via Telegram Bot API.
type TelegramProvider struct {
	botToken string
	chatID   string
	client   *http.Client
}

func NewTelegramProvider(botToken, chatID string) *TelegramProvider {
	return &TelegramProvider{
		botToken: botToken,
		chatID:   chatID,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *TelegramProvider) Name() string { return "telegram" }

func (p *TelegramProvider) Available() bool {
	return p.botToken != "" && p.chatID != ""
}

func (p *TelegramProvider) Send(ctx context.Context, msg Message) error {
	text := formatTelegramMessage(msg)

	payload := map[string]interface{}{
		"chat_id":    p.chatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", p.botToken)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("telegram error: status %d", resp.StatusCode)
	}

	return nil
}

func formatTelegramMessage(msg Message) string {
	icon := "ℹ️"
	switch msg.Severity {
	case "warning":
		icon = "⚠️"
	case "error":
		icon = "🔴"
	case "critical":
		icon = "🚨"
	}

	text := fmt.Sprintf("%s *%s*\n", icon, escapeTelegramMarkdown(msg.Title))
	if msg.Project != "" {
		text += fmt.Sprintf("📦 Project: `%s`\n", msg.Project)
	}
	text += "\n" + escapeTelegramMarkdown(msg.Body)

	if len(msg.Actions) > 0 {
		text += "\n\n"
		for _, a := range msg.Actions {
			text += fmt.Sprintf("→ %s: `%s`\n", a.Label, a.Command)
		}
	}

	return text
}

func escapeTelegramMarkdown(text string) string {
	// MarkdownV2 requires escaping special chars
	special := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	result := text
	for _, ch := range special {
		result = replaceAll(result, ch, "\\"+ch)
	}
	return result
}

func replaceAll(s, old, new string) string {
	result := ""
	for i := 0; i < len(s); i++ {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result += new
			i += len(old) - 1
		} else {
			result += string(s[i])
		}
	}
	return result
}
