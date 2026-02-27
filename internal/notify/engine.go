package notify

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/greencode/greenforge/internal/config"
)

// Engine dispatches notifications through configured channels.
type Engine struct {
	cfg       *config.NotifyConfig
	providers map[string]Provider
	mu        sync.RWMutex
}

// Provider is the interface for notification backends.
type Provider interface {
	Name() string
	Send(ctx context.Context, msg Message) error
	Available() bool
}

// Message represents a notification to be sent.
type Message struct {
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Severity  string            `json:"severity"` // info, warning, error, critical
	Project   string            `json:"project"`
	Event     string            `json:"event"` // pipeline_failure, pr_assigned, digest, etc.
	Actions   []Action          `json:"actions,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// Action is an interactive action in a notification.
type Action struct {
	Label   string `json:"label"`
	Command string `json:"command"` // greenforge command to execute
}

// NewEngine creates a notification engine from config.
func NewEngine(cfg *config.NotifyConfig) *Engine {
	e := &Engine{
		cfg:       cfg,
		providers: make(map[string]Provider),
	}

	// Initialize providers from config
	for _, ch := range cfg.Channels {
		if !ch.Enabled {
			continue
		}
		switch ch.Type {
		case "telegram":
			if ch.BotToken != "" && ch.ChatID != "" {
				e.providers["telegram"] = NewTelegramProvider(ch.BotToken, ch.ChatID)
			}
		case "email":
			if ch.Address != "" {
				e.providers["email"] = NewEmailProvider(ch.Address)
			}
		case "whatsapp":
			if ch.Phone != "" {
				e.providers["whatsapp"] = NewWhatsAppProvider(ch.Phone)
			}
		case "sms":
			if ch.Phone != "" {
				e.providers["sms"] = NewSMSProvider(ch.Phone)
			}
		case "cli":
			e.providers["cli"] = NewCLIProvider()
		}
	}

	// Always have CLI provider
	if _, exists := e.providers["cli"]; !exists {
		e.providers["cli"] = NewCLIProvider()
	}

	return e
}

// Send dispatches a notification to all configured channels.
func (e *Engine) Send(ctx context.Context, msg Message) error {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Check quiet hours
	if e.isQuietHours() && msg.Severity != "critical" {
		log.Printf("Notification suppressed (quiet hours): %s", msg.Title)
		return nil
	}

	// Check if event is enabled
	if !e.isEventEnabled(msg.Event) {
		return nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	var errs []error
	for name, provider := range e.providers {
		if !provider.Available() {
			continue
		}
		if err := provider.Send(ctx, msg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notification errors: %v", errs)
	}
	return nil
}

// SendTo dispatches a notification to specific channels only.
func (e *Engine) SendTo(ctx context.Context, msg Message, channels []string) error {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, chName := range channels {
		if provider, ok := e.providers[chName]; ok {
			if err := provider.Send(ctx, msg); err != nil {
				log.Printf("Notification error (%s): %v", chName, err)
			}
		}
	}
	return nil
}

func (e *Engine) isQuietHours() bool {
	if !e.cfg.QuietHours.Enabled {
		return false
	}
	now := time.Now()
	start, err1 := time.Parse("15:04", e.cfg.QuietHours.Start)
	end, err2 := time.Parse("15:04", e.cfg.QuietHours.End)
	if err1 != nil || err2 != nil {
		return false
	}

	currentMinutes := now.Hour()*60 + now.Minute()
	startMinutes := start.Hour()*60 + start.Minute()
	endMinutes := end.Hour()*60 + end.Minute()

	if startMinutes > endMinutes {
		// Crosses midnight (e.g., 22:00 - 07:00)
		return currentMinutes >= startMinutes || currentMinutes < endMinutes
	}
	return currentMinutes >= startMinutes && currentMinutes < endMinutes
}

func (e *Engine) isEventEnabled(event string) bool {
	switch event {
	case "pipeline_failure":
		return e.cfg.Events.PipelineFailures
	case "pr_assigned":
		return e.cfg.Events.PRAssigned
	case "all_commits":
		return e.cfg.Events.AllCommits
	case "autofix_completed":
		return e.cfg.Events.AutoFixCompleted
	case "digest":
		return true // Always allow digest
	default:
		return true
	}
}

// ListProviders returns names of configured providers.
func (e *Engine) ListProviders() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.providers))
	for name := range e.providers {
		names = append(names, name)
	}
	return names
}
