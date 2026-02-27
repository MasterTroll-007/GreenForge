// Package toolsdk provides the public SDK for writing custom GreenForge tools.
//
// Custom tools implement the Tool interface and are registered via TOOL.yaml manifests.
// Tools run inside Docker containers with sandboxed access to the workspace.
//
// Example:
//
//	type MyTool struct{}
//
//	func (t *MyTool) Execute(ctx context.Context, fn string, input json.RawMessage) (Result, error) {
//	    switch fn {
//	    case "my_function":
//	        return t.myFunction(ctx, input)
//	    default:
//	        return Result{}, fmt.Errorf("unknown function: %s", fn)
//	    }
//	}
package toolsdk

import (
	"context"
	"encoding/json"
	"time"
)

// Tool is the interface that custom tools must implement.
type Tool interface {
	// Name returns the unique tool identifier.
	Name() string

	// Execute runs a function within the tool.
	Execute(ctx context.Context, function string, input json.RawMessage) (Result, error)

	// Functions returns the list of available functions.
	Functions() []FunctionDef
}

// FunctionDef describes a single tool function.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Result is the output from a tool execution.
type Result struct {
	Output   string            `json:"output"`
	Error    string            `json:"error,omitempty"`
	Duration time.Duration     `json:"duration"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ToolContext provides access to workspace and configuration within a tool execution.
type ToolContext struct {
	WorkspaceDir string
	ProjectName  string
	Env          map[string]string
}

// GetToolContext extracts tool context from the execution context.
func GetToolContext(ctx context.Context) (*ToolContext, bool) {
	tc, ok := ctx.Value(ctxKeyToolContext{}).(*ToolContext)
	return tc, ok
}

// WithToolContext adds tool context to a context.
func WithToolContext(ctx context.Context, tc *ToolContext) context.Context {
	return context.WithValue(ctx, ctxKeyToolContext{}, tc)
}

type ctxKeyToolContext struct{}
