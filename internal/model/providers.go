package model

import "github.com/greencode/greenforge/internal/model/providers"

// NewOllamaProvider creates an Ollama provider.
func NewOllamaProvider(endpoint, model string) Provider {
	return providers.NewOllamaProvider(endpoint, model)
}

// NewAnthropicProvider creates an Anthropic Claude provider.
func NewAnthropicProvider(apiKey, model string) Provider {
	return providers.NewAnthropicProvider(apiKey, model)
}

// NewOpenAIProvider creates an OpenAI GPT provider.
func NewOpenAIProvider(apiKey, model string) Provider {
	return providers.NewOpenAIProvider(apiKey, model)
}
