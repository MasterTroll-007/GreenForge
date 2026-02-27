package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/greencode/greenforge/internal/agent"
	"github.com/greencode/greenforge/internal/audit"
	"github.com/greencode/greenforge/internal/sandbox"
	"gopkg.in/yaml.v3"
)

// Registry manages tool discovery, validation, and execution.
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]*ToolDef
	sandbox  *sandbox.Engine
	secrets  *sandbox.SecretManager
	auditor  *audit.Logger
}

// ToolDef represents a tool loaded from TOOL.yaml manifest.
type ToolDef struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       ToolSpec `yaml:"spec"`

	// handler is set for built-in tools (not loaded from YAML)
	handler BuiltinHandler `yaml:"-"`
}

type Metadata struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Category    string   `yaml:"category"`
	Tags        []string `yaml:"tags"`
}

type ToolSpec struct {
	Functions   []FunctionDef   `yaml:"functions"`
	Sandbox     SandboxSpec     `yaml:"sandbox"`
	Permissions []string        `yaml:"permissions"`
}

type FunctionDef struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Parameters  interface{} `yaml:"parameters"`
}

type SandboxSpec struct {
	Image      string            `yaml:"image"`
	Network    NetworkSpec       `yaml:"network"`
	Filesystem FilesystemSpec    `yaml:"filesystem"`
	Resources  ResourceSpec      `yaml:"resources"`
}

type NetworkSpec struct {
	Mode         string   `yaml:"mode"`
	AllowedHosts []string `yaml:"allowedHosts"`
}

type FilesystemSpec struct {
	Mounts []MountSpec `yaml:"mounts"`
}

type MountSpec struct {
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"readOnly"`
}

type ResourceSpec struct {
	CPULimit       string `yaml:"cpuLimit"`
	MemoryLimit    string `yaml:"memoryLimit"`
	TimeoutSeconds int    `yaml:"timeoutSeconds"`
}

// NewRegistry creates a tool registry.
func NewRegistry(sandbox *sandbox.Engine, secrets *sandbox.SecretManager, auditor *audit.Logger) *Registry {
	return &Registry{
		tools:   make(map[string]*ToolDef),
		sandbox: sandbox,
		secrets: secrets,
		auditor: auditor,
	}
}

// LoadFromDir discovers and loads all tools from a directory.
func (r *Registry) LoadFromDir(toolsDir string) error {
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading tools dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(toolsDir, entry.Name(), "TOOL.yaml")
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue
		}

		if err := r.LoadTool(manifestPath); err != nil {
			return fmt.Errorf("loading tool %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// LoadTool loads a single tool from its TOOL.yaml manifest.
func (r *Registry) LoadTool(manifestPath string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	var tool ToolDef
	if err := yaml.Unmarshal(data, &tool); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	if tool.Metadata.Name == "" {
		return fmt.Errorf("tool manifest missing name: %s", manifestPath)
	}

	r.mu.Lock()
	r.tools[tool.Metadata.Name] = &tool
	r.mu.Unlock()

	return nil
}

// RegisterBuiltin registers a built-in tool (not from YAML manifest).
func (r *Registry) RegisterBuiltin(name, description, category string, handler BuiltinHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[name] = &ToolDef{
		Metadata: Metadata{
			Name:        name,
			Description: description,
			Category:    category,
		},
		Spec: ToolSpec{
			Functions: []FunctionDef{
				{Name: name, Description: description},
			},
		},
		handler: handler,
	}
}

// BuiltinHandler is the signature for built-in tool implementations.
type BuiltinHandler func(ctx context.Context, input map[string]interface{}) (agent.ToolResult, error)

// Execute runs a tool by name.
func (r *Registry) Execute(ctx context.Context, toolName string, input map[string]interface{}) (agent.ToolResult, error) {
	r.mu.RLock()
	tool, exists := r.tools[toolName]
	r.mu.RUnlock()

	if !exists {
		return agent.ToolResult{}, fmt.Errorf("unknown tool: %s", toolName)
	}

	start := time.Now()

	// Audit: tool execution started
	if r.auditor != nil {
		r.auditor.Log(audit.Event{
			Action: "tool.execute",
			Tool:   toolName,
			Details: map[string]string{
				"category": tool.Metadata.Category,
			},
		})
	}

	var result agent.ToolResult
	var err error

	if tool.handler != nil {
		// Built-in tool
		result, err = tool.handler(ctx, input)
	} else if r.sandbox != nil {
		// Sandboxed tool
		result, err = r.executeSandboxed(ctx, tool, input)
	} else {
		err = fmt.Errorf("no execution method available for tool %s", toolName)
	}

	result.Duration = time.Since(start)

	return result, err
}

func (r *Registry) executeSandboxed(ctx context.Context, tool *ToolDef, input map[string]interface{}) (agent.ToolResult, error) {
	spec := tool.Spec.Sandbox

	// Build mounts
	var mounts []sandbox.Mount
	for _, m := range spec.Filesystem.Mounts {
		// Expand variables
		source := os.ExpandEnv(m.Source)
		mounts = append(mounts, sandbox.Mount{
			Source:   source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	// Build command from input
	command := buildCommand(tool.Metadata.Name, input)

	timeout := time.Duration(spec.Resources.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	runResult, err := r.sandbox.Run(ctx, sandbox.RunConfig{
		Image:    spec.Image,
		Command:  command,
		Mounts:   mounts,
		Network: sandbox.NetworkPolicy{
			Mode:         spec.Network.Mode,
			AllowedHosts: spec.Network.AllowedHosts,
		},
		CPULimit: spec.Resources.CPULimit,
		MemLimit: spec.Resources.MemoryLimit,
		Timeout:  timeout,
	})
	if err != nil {
		return agent.ToolResult{Error: err.Error()}, err
	}

	output := runResult.Stdout
	if runResult.Stderr != "" {
		output += "\n" + runResult.Stderr
	}

	result := agent.ToolResult{
		Output: output,
		Metadata: map[string]string{
			"exit_code": fmt.Sprintf("%d", runResult.ExitCode),
		},
	}

	if runResult.ExitCode != 0 {
		result.Error = fmt.Sprintf("tool exited with code %d", runResult.ExitCode)
	}

	return result, nil
}

// ListTools returns info about all registered tools.
func (r *Registry) ListTools() []agent.ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]agent.ToolInfo, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, agent.ToolInfo{
			Name:        tool.Metadata.Name,
			Description: tool.Metadata.Description,
			Category:    tool.Metadata.Category,
		})
	}
	return tools
}

// GetTool returns a tool definition by name.
func (r *Registry) GetTool(name string) (*ToolDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func buildCommand(toolName string, input map[string]interface{}) []string {
	// Default: run the tool binary with JSON input
	return []string{"/usr/local/bin/greenforge-tool", toolName}
}

