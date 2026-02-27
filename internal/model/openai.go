package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIProvider implements Provider for OpenAI GPT models.
type OpenAIProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewOpenAIProvider(apiKey, defaultModel string) *OpenAIProvider {
	if defaultModel == "" {
		defaultModel = "gpt-4o"
	}
	return &OpenAIProvider{
		apiKey: apiKey,
		model:  defaultModel,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Available() bool {
	return p.apiKey != ""
}

// Models queries OpenAI API for available models.
func (p *OpenAIProvider) Models() []string {
	if p.apiKey == "" {
		return []string{p.model}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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

	// Filter to chat models only
	var names []string
	for _, m := range result.Data {
		names = append(names, m.ID)
	}
	if len(names) == 0 {
		return []string{p.model}
	}
	return names
}

func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	messages := make([]openaiMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		om := openaiMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
		if msg.ToolCallID != "" {
			om.ToolCallID = msg.ToolCallID
			om.Role = "tool"
		}
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				inputJSON, _ := json.Marshal(tc.Input)
				om.ToolCalls = append(om.ToolCalls, openaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openaiFunction{
						Name:      tc.Name,
						Arguments: string(inputJSON),
					},
				})
			}
		}
		messages = append(messages, om)
	}

	apiReq := openaiRequest{
		Model:       p.resolveModel(req.Model),
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}

	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			apiReq.Tools = append(apiReq.Tools, openaiTool{
				Type: "function",
				Function: openaiToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Schema,
				},
			})
		}
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("openai error %d: %s", httpResp.StatusCode, string(respBody))
	}

	var apiResp openaiResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	choice := apiResp.Choices[0]
	resp := &Response{
		Content: choice.Message.Content,
		Model:   apiResp.Model,
		Usage: Usage{
			InputTokens:  apiResp.Usage.PromptTokens,
			OutputTokens: apiResp.Usage.CompletionTokens,
		},
		FinishReason: choice.FinishReason,
	}

	for _, tc := range choice.Message.ToolCalls {
		var input map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &input)
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	return resp, nil
}

func (p *OpenAIProvider) StreamComplete(ctx context.Context, req Request, cb StreamCallback) error {
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return err
	}
	cb(StreamChunk{
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
		Done:      true,
	})
	return nil
}

func (p *OpenAIProvider) resolveModel(override string) string {
	if override != "" {
		return override
	}
	return p.model
}

// --- OpenAI API types ---

type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Tools       []openaiTool    `json:"tools,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
}

type openaiToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiTool struct {
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type openaiResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      openaiMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}
