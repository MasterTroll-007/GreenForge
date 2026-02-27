package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/greencode/greenforge/internal/model"
)

// AnthropicProvider implements model.Provider for Anthropic Claude API.
type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewAnthropicProvider(apiKey, defaultModel string) *AnthropicProvider {
	if defaultModel == "" {
		defaultModel = "claude-sonnet-4-20250514"
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  defaultModel,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Available() bool {
	return p.apiKey != ""
}

func (p *AnthropicProvider) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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

	resp := &model.Response{
		Model: apiResp.Model,
		Usage: model.Usage{
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
			resp.ToolCalls = append(resp.ToolCalls, model.ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}

	return resp, nil
}

func (p *AnthropicProvider) StreamComplete(ctx context.Context, req model.Request, cb model.StreamCallback) error {
	// For now, use non-streaming and send as single chunk
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return err
	}
	cb(model.StreamChunk{
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
		Done:      true,
	})
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
