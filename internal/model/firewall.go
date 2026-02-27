package model

import (
	"regexp"
	"strings"
)

// Firewall scrubs secrets and sensitive data before sending to AI models.
type Firewall struct {
	patterns []*regexp.Regexp
	keywords []string
}

// NewFirewall creates a firewall with default secret detection patterns.
func NewFirewall() *Firewall {
	return &Firewall{
		patterns: compilePatterns(defaultPatterns),
		keywords: defaultKeywords,
	}
}

var defaultPatterns = []string{
	// API keys and tokens
	`(?i)(api[_-]?key|apikey)\s*[:=]\s*['"]?([A-Za-z0-9_\-]{20,})['"]?`,
	`(?i)(secret|token|password|passwd|pwd)\s*[:=]\s*['"]?([^\s'"]{8,})['"]?`,
	`(?i)(bearer\s+)[A-Za-z0-9_\-\.]{20,}`,

	// AWS
	`AKIA[0-9A-Z]{16}`,
	`(?i)aws[_-]?secret[_-]?access[_-]?key\s*[:=]\s*['"]?([A-Za-z0-9/+=]{40})['"]?`,

	// Azure
	`(?i)(DefaultEndpointsProtocol=https;AccountName=)[^\s;]+`,
	`(?i)(azure[_-]?(?:storage|devops|ad)[_-]?(?:key|token|secret|password))\s*[:=]\s*['"]?([^\s'"]{8,})['"]?`,

	// JDBC connection strings with passwords
	`(?i)jdbc:[a-z]+://[^\s]*password=[^\s&;]+`,

	// Private keys
	`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`,

	// GitHub/GitLab tokens
	`gh[ps]_[A-Za-z0-9_]{36,}`,
	`glpat-[A-Za-z0-9_\-]{20,}`,

	// Anthropic/OpenAI keys
	`sk-ant-[A-Za-z0-9_\-]{20,}`,
	`sk-[A-Za-z0-9]{20,}`,
}

var defaultKeywords = []string{
	"password", "passwd", "secret", "api_key", "apikey",
	"access_token", "auth_token", "credentials",
	"private_key", "client_secret",
}

func compilePatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

// ScrubRequest sanitizes all messages in a request.
func (f *Firewall) ScrubRequest(req Request) Request {
	sanitized := Request{
		Tools:       req.Tools,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Model:       req.Model,
	}

	sanitized.Messages = make([]Message, len(req.Messages))
	for i, msg := range req.Messages {
		sanitized.Messages[i] = Message{
			Role:       msg.Role,
			Content:    f.ScrubText(msg.Content),
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}
	}

	return sanitized
}

// ScrubText replaces detected secrets in text with redacted placeholders.
func (f *Firewall) ScrubText(text string) string {
	result := text

	for _, pattern := range f.patterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Keep the key name, redact the value
			if idx := strings.IndexAny(match, ":="); idx >= 0 {
				return match[:idx+1] + " [REDACTED]"
			}
			if strings.HasPrefix(match, "-----BEGIN") {
				return "[REDACTED PRIVATE KEY]"
			}
			return "[REDACTED]"
		})
	}

	return result
}

// ContainsSecret checks if text likely contains secrets.
func (f *Firewall) ContainsSecret(text string) bool {
	for _, pattern := range f.patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// AddPattern adds a custom secret detection pattern.
func (f *Firewall) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	f.patterns = append(f.patterns, re)
	return nil
}
