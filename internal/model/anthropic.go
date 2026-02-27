package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultAnthropicAPI = "https://api.anthropic.com"

// AnthropicProvider implements Provider for Anthropic Claude API.
type AnthropicProvider struct {
	mu           sync.RWMutex
	accessToken  string
	refreshToken string
	expiresAt    int64 // unix ms
	isOAuth      bool
	model        string
	baseURL      string // API base URL (can be proxy)
	client       *http.Client
}

// NewAnthropicProvider creates a provider with a regular API key.
func NewAnthropicProvider(apiKey, defaultModel string) *AnthropicProvider {
	if defaultModel == "" {
		defaultModel = "claude-sonnet-4-6"
	}
	baseURL := os.Getenv("ANTHROPIC_PROXY")
	if baseURL == "" {
		baseURL = defaultAnthropicAPI
	}
	return &AnthropicProvider{
		accessToken: apiKey,
		isOAuth:     strings.HasPrefix(apiKey, "sk-ant-oat"),
		model:       defaultModel,
		baseURL:     baseURL,
		client:      &http.Client{Timeout: 5 * time.Minute},
	}
}

// NewAnthropicOAuthProvider creates a provider from Claude Code OAuth credentials.
func NewAnthropicOAuthProvider(accountFile, defaultModel string) (*AnthropicProvider, error) {
	if defaultModel == "" {
		defaultModel = "claude-sonnet-4-6"
	}

	baseURL := os.Getenv("ANTHROPIC_PROXY")
	if baseURL == "" {
		baseURL = defaultAnthropicAPI
	}
	p := &AnthropicProvider{
		isOAuth: true,
		model:   defaultModel,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}

	if err := p.loadOAuthFromFile(accountFile); err != nil {
		return nil, fmt.Errorf("loading OAuth credentials: %w", err)
	}

	return p, nil
}

// loadOAuthFromFile reads Claude Code account JSON file.
func (p *AnthropicProvider) loadOAuthFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var account struct {
		ClaudeAiOauth *struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}

	if err := json.Unmarshal(data, &account); err != nil {
		return fmt.Errorf("parsing account file: %w", err)
	}

	if account.ClaudeAiOauth == nil {
		return fmt.Errorf("no claudeAiOauth in account file")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.accessToken = account.ClaudeAiOauth.AccessToken
	p.refreshToken = account.ClaudeAiOauth.RefreshToken
	p.expiresAt = account.ClaudeAiOauth.ExpiresAt

	return nil
}

// refreshIfNeeded checks token expiry and refreshes if needed.
func (p *AnthropicProvider) refreshIfNeeded() error {
	if !p.isOAuth {
		return nil
	}

	p.mu.RLock()
	expiresAt := p.expiresAt
	hasToken := p.accessToken != ""
	hasRefresh := p.refreshToken != ""
	p.mu.RUnlock()

	// If no refresh token or token not expiring soon, skip
	if !hasRefresh {
		return nil
	}

	// expiresAt could be in seconds or milliseconds; normalize
	if expiresAt > 0 && expiresAt < 1e12 {
		expiresAt = expiresAt * 1000 // was in seconds
	}

	// Refresh if less than 5 minutes remaining
	if expiresAt > 0 && time.Now().UnixMilli() < expiresAt-5*60*1000 {
		return nil // token still valid
	}

	// If expiresAt is 0 and we have a token, try using it without refresh
	if expiresAt == 0 && hasToken {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Use standard OAuth2 form-encoded format
	formData := fmt.Sprintf("grant_type=refresh_token&refresh_token=%s", p.refreshToken)
	resp, err := p.client.Post(
		"https://console.anthropic.com/v1/oauth/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(formData),
	)
	if err != nil {
		// If refresh fails but we have an existing token, use it
		if p.accessToken != "" {
			return nil
		}
		return fmt.Errorf("OAuth refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// If refresh fails but we have an existing token, try using it
		if p.accessToken != "" {
			return nil
		}
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OAuth refresh failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil // Silently fail, use existing token
	}

	if tokenResp.AccessToken != "" {
		p.accessToken = tokenResp.AccessToken
	}
	if tokenResp.RefreshToken != "" {
		p.refreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		p.expiresAt = time.Now().UnixMilli() + tokenResp.ExpiresIn*1000
	}

	return nil
}

// FindClaudeCodeAccount finds the active Claude Code OAuth account file.
func FindClaudeCodeAccount() string {
	// Check env override first
	if path := os.Getenv("CLAUDE_ACCOUNT_FILE"); path != "" {
		return path
	}

	// Standard Claude Code config locations
	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")

	// Read current account name
	currentData, err := os.ReadFile(filepath.Join(claudeDir, "current-account.txt"))
	if err != nil {
		return ""
	}
	current := strings.TrimSpace(string(currentData))
	if current == "" {
		return ""
	}

	accountFile := filepath.Join(claudeDir, "accounts", current+".json")
	if _, err := os.Stat(accountFile); err == nil {
		return accountFile
	}

	return ""
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Available() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.accessToken != ""
}

// setAuthHeaders sets the appropriate auth headers based on token type.
func (p *AnthropicProvider) setAuthHeaders(req *http.Request, token string, isOAuth bool) {
	// When using a proxy, skip auth headers - proxy handles auth via claude CLI
	if p.baseURL != defaultAnthropicAPI {
		return
	}
	if isOAuth {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("x-api-key", token)
	}
}

// Models queries the proxy or Anthropic API for available models dynamically.
func (p *AnthropicProvider) Models() []string {
	if err := p.refreshIfNeeded(); err != nil {
		return []string{p.model}
	}

	// Longer timeout for proxy (it may need to fetch from docs/API)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/v1/models?limit=100", nil)
	req.Header.Set("anthropic-version", "2023-06-01")

	p.mu.RLock()
	token := p.accessToken
	isOAuth := p.isOAuth
	p.mu.RUnlock()

	p.setAuthHeaders(req, token, isOAuth)

	resp, err := p.client.Do(req)
	if err != nil {
		return []string{p.model}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return []string{p.model}
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return []string{p.model}
	}

	names := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		names = append(names, m.ID)
	}
	if len(names) == 0 {
		return []string{p.model}
	}
	return names
}

func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	// Extract system message
	var system string
	var messages []anthropicMessage
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			system = msg.Content
			continue
		}

		am := anthropicMessage{Role: msg.Role}
		if msg.ToolCallID != "" {
			am.Content = []anthropicContent{{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   msg.Content,
			}}
		} else if len(msg.ToolCalls) > 0 {
			am.Role = "assistant"
			for _, tc := range msg.ToolCalls {
				am.Content = append(am.Content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				})
			}
			if msg.Content != "" {
				am.Content = append([]anthropicContent{{
					Type: "text",
					Text: msg.Content,
				}}, am.Content...)
			}
		} else {
			am.Content = []anthropicContent{{Type: "text", Text: msg.Content}}
		}
		messages = append(messages, am)
	}

	apiReq := anthropicRequest{
		Model:     p.resolveModel(req.Model),
		MaxTokens: req.MaxTokens,
		System:    system,
		Messages:  messages,
		CWD:       req.WorkingDir,
	}

	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			apiReq.Tools = append(apiReq.Tools, anthropicTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Schema,
			})
		}
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, err
	}

	// Refresh OAuth token if needed
	if err := p.refreshIfNeeded(); err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	p.mu.RLock()
	token := p.accessToken
	isOAuth := p.isOAuth
	p.mu.RUnlock()

	p.setAuthHeaders(httpReq, token, isOAuth)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("anthropic error %d: %s", httpResp.StatusCode, string(respBody))
	}

	var apiResp anthropicResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	resp := &Response{
		Model: apiResp.Model,
		Usage: Usage{
			InputTokens:  apiResp.Usage.InputTokens,
			OutputTokens: apiResp.Usage.OutputTokens,
		},
		FinishReason: apiResp.StopReason,
	}

	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			resp.Content += block.Text
		case "tool_use":
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}

	return resp, nil
}

func (p *AnthropicProvider) StreamComplete(ctx context.Context, req Request, cb StreamCallback) error {
	// Extract system message
	var system string
	var messages []anthropicMessage
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			system = msg.Content
			continue
		}
		am := anthropicMessage{Role: msg.Role}
		if msg.Content != "" {
			am.Content = []anthropicContent{{Type: "text", Text: msg.Content}}
		}
		messages = append(messages, am)
	}

	apiReq := anthropicStreamRequest{
		Model:     p.resolveModel(req.Model),
		MaxTokens: req.MaxTokens,
		System:    system,
		Messages:  messages,
		Stream:    true,
		CWD:       req.WorkingDir,
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return err
	}

	if err := p.refreshIfNeeded(); err != nil {
		return fmt.Errorf("token refresh: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	p.mu.RLock()
	token := p.accessToken
	isOAuth := p.isOAuth
	p.mu.RUnlock()

	p.setAuthHeaders(httpReq, token, isOAuth)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("anthropic stream request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("anthropic stream error %d: %s", httpResp.StatusCode, string(respBody))
	}

	// Detect proxy SSE vs native Anthropic SSE
	contentType := httpResp.Header.Get("Content-Type")
	isProxySSE := strings.Contains(contentType, "text/event-stream") && p.baseURL != defaultAnthropicAPI

	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	if isProxySSE {
		// Parse proxy SSE events (event: type\ndata: json\n\n)
		var currentEvent string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				currentEvent = strings.TrimPrefix(line, "event: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var payload struct {
				Text    string `json:"text"`
				Name    string `json:"name"`
				Content string `json:"content"`
				Message string `json:"message"`
				Model   string `json:"model"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				continue
			}

			switch currentEvent {
			case "text":
				if payload.Text != "" {
					cb(StreamChunk{Content: payload.Text})
				}
			case "tool_use":
				if payload.Name != "" {
					cb(StreamChunk{
						ToolCalls: []ToolCall{{Name: payload.Name}},
					})
				}
			case "tool_result":
				// Optionally show tool result summary
			case "error":
				if payload.Message != "" {
					cb(StreamChunk{Content: "\n[Error: " + payload.Message + "]\n"})
				}
			case "done":
				cb(StreamChunk{Done: true})
				return nil
			}
		}
		cb(StreamChunk{Done: true})
		return nil
	}

	// Native Anthropic SSE
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event struct {
			Type  string `json:"type"`
			Delta *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta != nil && event.Delta.Text != "" {
				cb(StreamChunk{Content: event.Delta.Text})
			}
		case "message_stop":
			cb(StreamChunk{Done: true})
			return nil
		}
	}

	cb(StreamChunk{Done: true})
	return nil
}

func (p *AnthropicProvider) resolveModel(override string) string {
	if override != "" {
		return override
	}
	return p.model
}

// --- Anthropic API types ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	CWD       string             `json:"cwd,omitempty"` // Working directory for proxy
}

type anthropicStreamRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
	CWD       string             `json:"cwd,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

type anthropicResponse struct {
	ID         string             `json:"id"`
	Model      string             `json:"model"`
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
