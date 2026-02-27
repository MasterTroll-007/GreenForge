package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/greencode/greenforge/internal/config"
	"github.com/greencode/greenforge/internal/model"
)

// Runtime implements the agent loop: plan → execute → observe → respond.
type Runtime struct {
	cfg       *config.Config
	router    *model.Router
	memory    *Memory
	toolExec  ToolExecutor
	callbacks Callbacks
}

// ToolExecutor is the interface for executing tools from the agent loop.
type ToolExecutor interface {
	Execute(ctx context.Context, toolName string, input map[string]interface{}) (ToolResult, error)
	ListTools() []ToolInfo
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	Output   string
	Error    string
	Duration time.Duration
	Metadata map[string]string
}

// ToolInfo describes an available tool.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// Callbacks for streaming responses back to the caller.
type Callbacks struct {
	OnThinking  func(text string)
	OnResponse  func(text string)
	OnToolCall  func(toolName string, input map[string]interface{})
	OnToolResult func(toolName string, result ToolResult)
	OnError     func(err error)
	OnDone      func()
}

// NewRuntime creates a new agent runtime.
func NewRuntime(cfg *config.Config, router *model.Router) *Runtime {
	return &Runtime{
		cfg:    cfg,
		router: router,
		memory: NewMemory(),
	}
}

// SetToolExecutor sets the tool executor for the agent.
func (r *Runtime) SetToolExecutor(exec ToolExecutor) {
	r.toolExec = exec
}

// SetCallbacks configures streaming callbacks.
func (r *Runtime) SetCallbacks(cb Callbacks) {
	r.callbacks = cb
}

// ProcessMessage runs one iteration of the agent loop for a user message.
func (r *Runtime) ProcessMessage(ctx context.Context, sessionID string, message string) error {
	// Add user message to memory
	r.memory.Add(sessionID, Message{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	})

	// Build context for the model
	promptCtx := r.buildContext(sessionID)

	// Agent loop: iterate until we get a final response (no more tool calls)
	maxIterations := 20
	for i := 0; i < maxIterations; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Call the model
		if r.callbacks.OnThinking != nil {
			r.callbacks.OnThinking("Thinking...")
		}

		resp, err := r.router.Complete(ctx, model.Request{
			Messages:    promptCtx,
			Tools:       r.getToolDefs(),
			MaxTokens:   4096,
			Temperature: 0.1,
		})
		if err != nil {
			if r.callbacks.OnError != nil {
				r.callbacks.OnError(err)
			}
			return fmt.Errorf("model completion error: %w", err)
		}

		// Check if response contains tool calls
		if len(resp.ToolCalls) == 0 {
			// Final response - send to user
			r.memory.Add(sessionID, Message{
				Role:      "assistant",
				Content:   resp.Content,
				Timestamp: time.Now(),
			})

			if r.callbacks.OnResponse != nil {
				r.callbacks.OnResponse(resp.Content)
			}
			if r.callbacks.OnDone != nil {
				r.callbacks.OnDone()
			}
			return nil
		}

		// Execute tool calls
		r.memory.Add(sessionID, Message{
			Role:      "assistant",
			Content:   resp.Content,
			Timestamp: time.Now(),
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			if r.callbacks.OnToolCall != nil {
				r.callbacks.OnToolCall(tc.Name, tc.Input)
			}

			result, err := r.executeTool(ctx, tc)
			if err != nil {
				log.Printf("Tool execution error: %v", err)
				result = ToolResult{Error: err.Error()}
			}

			if r.callbacks.OnToolResult != nil {
				r.callbacks.OnToolResult(tc.Name, result)
			}

			// Add tool result to context
			content := result.Output
			if result.Error != "" {
				content = fmt.Sprintf("Error: %s", result.Error)
			}
			r.memory.Add(sessionID, Message{
				Role:       "tool",
				Content:    content,
				Timestamp:  time.Now(),
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})

			promptCtx = r.buildContext(sessionID)
		}
	}

	return fmt.Errorf("agent loop exceeded max iterations (%d)", maxIterations)
}

func (r *Runtime) buildContext(sessionID string) []model.Message {
	history := r.memory.Get(sessionID)

	// Build system prompt
	systemPrompt := r.buildSystemPrompt()

	messages := []model.Message{
		{Role: "system", Content: systemPrompt},
	}

	for _, msg := range history {
		m := model.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
		if len(msg.ToolCalls) > 0 {
			m.ToolCalls = msg.ToolCalls
		}
		if msg.ToolCallID != "" {
			m.ToolCallID = msg.ToolCallID
		}
		messages = append(messages, m)
	}

	return messages
}

func (r *Runtime) buildSystemPrompt() string {
	prompt := `You are GreenForge, a secure AI developer agent specialized for JVM teams.
You help developers with Spring Boot, Kafka, Gradle/Maven projects.

You have access to tools for:
- Git operations (status, diff, commit, log, blame)
- File operations (read, write, search)
- Shell commands (sandboxed)
- Build operations (Gradle/Maven)
- Spring analysis (endpoints, beans, config)
- Kafka mapping (topics, listeners, flows)
- Database operations (query, schema, migrations)

Guidelines:
- Be concise and helpful
- Use tools when needed to answer questions
- Always explain what you're doing
- Never expose secrets or credentials
- Prefer reading files over guessing
`
	// Add tool descriptions
	if r.toolExec != nil {
		prompt += "\nAvailable tools:\n"
		for _, tool := range r.toolExec.ListTools() {
			prompt += fmt.Sprintf("- %s: %s\n", tool.Name, tool.Description)
		}
	}

	return prompt
}

func (r *Runtime) getToolDefs() []model.ToolDef {
	if r.toolExec == nil {
		return nil
	}

	var defs []model.ToolDef
	for _, tool := range r.toolExec.ListTools() {
		defs = append(defs, model.ToolDef{
			Name:        tool.Name,
			Description: tool.Description,
		})
	}
	return defs
}

func (r *Runtime) executeTool(ctx context.Context, tc model.ToolCall) (ToolResult, error) {
	if r.toolExec == nil {
		return ToolResult{}, fmt.Errorf("no tool executor configured")
	}
	return r.toolExec.Execute(ctx, tc.Name, tc.Input)
}
