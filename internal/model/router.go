package model

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/greencode/greenforge/internal/config"
)

// Router selects and routes requests to the appropriate AI model provider
// based on project policies.
type Router struct {
	cfg       *config.Config
	providers map[string]Provider
	firewall  *Firewall
}

// Provider is the interface all AI model backends must implement.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (*Response, error)
	StreamComplete(ctx context.Context, req Request, cb StreamCallback) error
	Available() bool
	// Models returns dynamically discovered models from this provider.
	Models() []string
}

// StreamCallback receives streaming chunks.
type StreamCallback func(chunk StreamChunk)

// StreamChunk is a streaming response fragment.
type StreamChunk struct {
	Content   string
	ToolCalls []ToolCall
	Done      bool
}

// Request represents a model completion request.
type Request struct {
	Messages    []Message   `json:"messages"`
	Tools       []ToolDef   `json:"tools,omitempty"`
	MaxTokens   int         `json:"max_tokens"`
	Temperature float64     `json:"temperature"`
	Model       string      `json:"model,omitempty"`
	WorkingDir  string      `json:"working_dir,omitempty"` // Project workspace for file access
}

// Message is a chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Response from a model completion.
type Response struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Model      string     `json:"model"`
	Usage      Usage      `json:"usage"`
	FinishReason string   `json:"finish_reason"`
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// ToolDef describes a tool for the model.
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Schema      interface{} `json:"input_schema,omitempty"`
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// NewRouter creates a model router with configured providers.
func NewRouter(cfg *config.Config) *Router {
	r := &Router{
		cfg:       cfg,
		providers: make(map[string]Provider),
		firewall:  NewFirewall(),
	}

	// Initialize providers from config
	for _, pc := range cfg.AI.Providers {
		switch pc.Name {
		case "ollama":
			endpoint := pc.Endpoint
			if endpoint == "" {
				endpoint = "http://localhost:11434"
			}
			r.providers["ollama"] = NewOllamaProvider(endpoint, pc.Model)
		case "anthropic":
			if pc.APIKey != "" {
				r.providers["anthropic"] = NewAnthropicProvider(pc.APIKey, pc.Model)
			}
		case "openai":
			r.providers["openai"] = NewOpenAIProvider(pc.APIKey, pc.Model)
		}
	}

	// Try to auto-detect Claude Code OAuth if no Anthropic provider configured
	if _, exists := r.providers["anthropic"]; !exists {
		if accountFile := FindClaudeCodeAccount(); accountFile != "" {
			if p, err := NewAnthropicOAuthProvider(accountFile, "claude-sonnet-4-6"); err == nil {
				r.providers["anthropic"] = p
			}
		}
	}

	// Always register ollama as fallback
	if _, exists := r.providers["ollama"]; !exists {
		r.providers["ollama"] = NewOllamaProvider("http://localhost:11434", "codestral")
	}

	return r
}

// Complete sends a request to the appropriate provider.
func (r *Router) Complete(ctx context.Context, req Request) (*Response, error) {
	provider, err := r.selectProvider(ctx, req.Model)
	if err != nil {
		return nil, err
	}

	// Apply firewall: scrub secrets from messages
	sanitized := r.firewall.ScrubRequest(req)

	resp, err := provider.Complete(ctx, sanitized)
	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", provider.Name(), err)
	}

	return resp, nil
}

// StreamComplete sends a streaming request.
func (r *Router) StreamComplete(ctx context.Context, req Request, cb StreamCallback) error {
	provider, err := r.selectProvider(ctx, req.Model)
	if err != nil {
		return err
	}

	sanitized := r.firewall.ScrubRequest(req)
	return provider.StreamComplete(ctx, sanitized, cb)
}

func (r *Router) selectProvider(ctx context.Context, modelOverride string) (Provider, error) {
	// If explicit model requested (e.g. "ollama/codestral")
	if modelOverride != "" {
		parts := strings.SplitN(modelOverride, "/", 2)
		providerName := parts[0]
		if p, ok := r.providers[providerName]; ok {
			return p, nil
		}
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	// Check project-based model policy
	projectPath := ctx.Value(ctxKeyProject{})
	if projectPath != nil {
		if pp, ok := projectPath.(string); ok {
			provider := r.resolveByPolicy(pp)
			if provider != nil {
				return provider, nil
			}
		}
	}

	// Use default model
	defaultModel := r.cfg.AI.DefaultModel
	if defaultModel != "" {
		parts := strings.SplitN(defaultModel, "/", 2)
		if p, ok := r.providers[parts[0]]; ok {
			return p, nil
		}
	}

	// Fallback: try anthropic, then ollama
	if p, ok := r.providers["anthropic"]; ok && p.Available() {
		return p, nil
	}
	if p, ok := r.providers["ollama"]; ok {
		return p, nil
	}

	return nil, fmt.Errorf("no available AI model provider")
}

func (r *Router) resolveByPolicy(projectPath string) Provider {
	for _, policy := range r.cfg.AI.Policies {
		matched, _ := filepath.Match(policy.ProjectPattern, projectPath)
		if matched {
			for _, allowed := range policy.AllowedProviders {
				if p, ok := r.providers[allowed]; ok && p.Available() {
					return p
				}
			}
		}
	}
	return nil
}

// ListProviders returns names of configured providers.
func (r *Router) ListProviders() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// ModelInfo describes an available model for display.
type ModelInfo struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Active   bool   `json:"active"`
	Status   string `json:"status"`
}

// ListModels dynamically queries all providers for their available models.
func (r *Router) ListModels() []ModelInfo {
	var models []ModelInfo
	current := r.cfg.AI.DefaultModel

	for name, p := range r.providers {
		status := "ready"
		if !p.Available() {
			status = "unavailable"
		}

		for _, m := range p.Models() {
			id := name + "/" + m
			models = append(models, ModelInfo{
				ID:       id,
				Provider: name,
				Model:    m,
				Active:   id == current,
				Status:   status,
			})
		}
	}

	return models
}

// SetDefaultModel changes the active model.
func (r *Router) SetDefaultModel(modelID string) error {
	parts := strings.SplitN(modelID, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid model format, use: provider/model (e.g. anthropic/claude-sonnet-4-20250514)")
	}
	providerName := parts[0]
	if _, ok := r.providers[providerName]; !ok {
		return fmt.Errorf("unknown provider: %s", providerName)
	}
	r.cfg.AI.DefaultModel = modelID
	return nil
}

// GetDefaultModel returns current default model ID.
func (r *Router) GetDefaultModel() string {
	return r.cfg.AI.DefaultModel
}

type ctxKeyProject struct{}

// WithProject adds project path to context for policy resolution.
func WithProject(ctx context.Context, projectPath string) context.Context {
	return context.WithValue(ctx, ctxKeyProject{}, projectPath)
}
