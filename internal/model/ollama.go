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

// OllamaProvider implements the Provider interface for Ollama.
type OllamaProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

func NewOllamaProvider(endpoint, defaultModel string) *OllamaProvider {
	if defaultModel == "" {
		defaultModel = "codestral"
	}
	return &OllamaProvider{
		endpoint: endpoint,
		model:    defaultModel,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", p.endpoint+"/api/tags", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// Models returns locally available Ollama models via /api/tags.
func (p *OllamaProvider) Models() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", p.endpoint+"/api/tags", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return []string{p.model}
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return []string{p.model}
	}

	names := make([]string, 0, len(result.Models))
	for _, m := range result.Models {
		names = append(names, m.Name)
	}
	if len(names) == 0 {
		return []string{p.model}
	}
	return names
}

func (p *OllamaProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	ollamaReq := ollamaChatRequest{
		Model:    p.resolveModel(req.Model),
		Messages: convertMessages(req.Messages),
		Stream:   false,
		Options: ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxTokens,
		},
	}

	if len(req.Tools) > 0 {
		ollamaReq.Tools = convertTools(req.Tools)
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("ollama error %d: %s", httpResp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	resp := &Response{
		Content: ollamaResp.Message.Content,
		Model:   ollamaResp.Model,
		Usage: Usage{
			InputTokens:  ollamaResp.PromptEvalCount,
			OutputTokens: ollamaResp.EvalCount,
		},
	}

	// Convert tool calls
	if len(ollamaResp.Message.ToolCalls) > 0 {
		for i, tc := range ollamaResp.Message.ToolCalls {
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:    fmt.Sprintf("call_%d", i),
				Name:  tc.Function.Name,
				Input: tc.Function.Arguments,
			})
		}
	}

	return resp, nil
}

func (p *OllamaProvider) StreamComplete(ctx context.Context, req Request, cb StreamCallback) error {
	ollamaReq := ollamaChatRequest{
		Model:    p.resolveModel(req.Model),
		Messages: convertMessages(req.Messages),
		Stream:   true,
		Options: ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxTokens,
		},
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	decoder := json.NewDecoder(httpResp.Body)
	for {
		var chunk ollamaChatResponse
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		cb(StreamChunk{
			Content: chunk.Message.Content,
			Done:    chunk.Done,
		})

		if chunk.Done {
			break
		}
	}

	return nil
}

func (p *OllamaProvider) resolveModel(override string) string {
	if override != "" {
		return override
	}
	return p.model
}

// --- Ollama API types ---

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Options  ollamaOptions   `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ollamaTool struct {
	Type     string         `json:"type"`
	Function ollamaFunction `json:"function"`
}

type ollamaFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaChatResponse struct {
	Model           string        `json:"model"`
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
}

func convertMessages(msgs []Message) []ollamaMessage {
	result := make([]ollamaMessage, len(msgs))
	for i, msg := range msgs {
		result[i] = ollamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}
	return result
}

func convertTools(tools []ToolDef) []ollamaTool {
	result := make([]ollamaTool, len(tools))
	for i, tool := range tools {
		result[i] = ollamaTool{
			Type: "function",
			Function: ollamaFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Schema,
			},
		}
	}
	return result
}
