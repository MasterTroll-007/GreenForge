package sandbox

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/greencode/greenforge/internal/config"
)

// Engine manages Docker sandbox containers for tool execution.
type Engine struct {
	cfg    *config.SandboxConfig
	client *client.Client
}

// NewEngine creates a new sandbox engine.
func NewEngine(cfg *config.SandboxConfig) (*Engine, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connecting to Docker: %w", err)
	}

	// Verify Docker is running
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("Docker not available: %w", err)
	}

	return &Engine{cfg: cfg, client: cli}, nil
}

// RunConfig defines how to run a tool in a sandbox.
type RunConfig struct {
	Image      string
	Command    []string
	WorkDir    string
	Env        map[string]string // secrets injected as env vars
	Mounts     []Mount
	Network    NetworkPolicy
	CPULimit   string
	MemLimit   string
	Timeout    time.Duration
	ReadOnly   bool
}

// Mount represents a filesystem mount.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// NetworkPolicy defines network access.
type NetworkPolicy struct {
	Mode         string   // none, restricted, host
	AllowedHosts []string // for restricted mode
}

// RunResult contains the output from sandbox execution.
type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

// Run executes a command in a sandboxed Docker container.
func (e *Engine) Run(ctx context.Context, rc RunConfig) (*RunResult, error) {
	if !e.cfg.Enabled {
		return nil, fmt.Errorf("sandbox is disabled in config")
	}

	start := time.Now()
	containerName := fmt.Sprintf("gf-tool-%s", uuid.New().String()[:8])

	// Build environment variables
	env := make([]string, 0, len(rc.Env))
	for k, v := range rc.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Configure resource limits
	resources := container.Resources{}
	if rc.CPULimit != "" {
		// Parse CPU limit as nanoCPUs
		resources.NanoCPUs = parseCPULimit(rc.CPULimit)
	}
	if rc.MemLimit != "" {
		resources.Memory = parseMemLimit(rc.MemLimit)
	}

	// Build mounts
	var binds []string
	for _, m := range rc.Mounts {
		bind := fmt.Sprintf("%s:%s", m.Source, m.Target)
		if m.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}

	// Network mode
	netMode := container.NetworkMode("none")
	switch rc.Network.Mode {
	case "host":
		netMode = "host"
	case "restricted":
		netMode = "bridge" // We'll add firewall rules below
	}

	// Create container
	containerCfg := &container.Config{
		Image:      rc.Image,
		Cmd:        rc.Command,
		WorkingDir: rc.WorkDir,
		Env:        env,
		Tty:        false,
	}

	hostCfg := &container.HostConfig{
		Binds:       binds,
		NetworkMode: netMode,
		Resources:   resources,
		ReadonlyRootfs: rc.ReadOnly,
		AutoRemove:  true,
		SecurityOpt: []string{"no-new-privileges"},
	}

	// Apply timeout
	timeout := rc.Timeout
	if timeout == 0 {
		timeout = e.cfg.Timeout.Duration
	}
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := e.client.ContainerCreate(timeoutCtx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("creating container: %w", err)
	}

	// Start container
	if err := e.client.ContainerStart(timeoutCtx, resp.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("starting container: %w", err)
	}

	// Wait for completion
	statusCh, errCh := e.client.ContainerWait(timeoutCtx, resp.ID, container.WaitConditionNotRunning)

	var exitCode int
	select {
	case err := <-errCh:
		if err != nil {
			// Try to kill container on error
			e.client.ContainerKill(context.Background(), resp.ID, "KILL")
			return nil, fmt.Errorf("waiting for container: %w", err)
		}
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
	case <-timeoutCtx.Done():
		e.client.ContainerKill(context.Background(), resp.ID, "KILL")
		return nil, fmt.Errorf("tool execution timed out after %s", timeout)
	}

	// Get logs
	logReader, err := e.client.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		log.Printf("Warning: could not read container logs: %v", err)
	}
	defer logReader.Close()

	logData, _ := io.ReadAll(logReader)
	stdout, stderr := splitDockerLogs(string(logData))

	return &RunResult{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		Duration: time.Since(start),
	}, nil
}

// Available checks if Docker is running and accessible.
func (e *Engine) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := e.client.Ping(ctx)
	return err == nil
}

// PullImage ensures a tool image is available.
func (e *Engine) PullImage(ctx context.Context, image string) error {
	reader, err := e.client.ImagePull(ctx, image, nil)
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", image, err)
	}
	if reader != nil {
		defer reader.Close()
		io.Copy(io.Discard, reader) // Wait for pull to complete
	}
	return nil
}

// Close releases the Docker client.
func (e *Engine) Close() error {
	return e.client.Close()
}

// --- Helpers ---

func parseCPULimit(limit string) int64 {
	// "2.0" → 2000000000 nanoCPUs
	var cpus float64
	fmt.Sscanf(limit, "%f", &cpus)
	return int64(cpus * 1e9)
}

func parseMemLimit(limit string) int64 {
	limit = strings.TrimSpace(limit)
	var value int64
	if strings.HasSuffix(limit, "g") || strings.HasSuffix(limit, "G") {
		fmt.Sscanf(limit, "%d", &value)
		return value * 1024 * 1024 * 1024
	}
	if strings.HasSuffix(limit, "m") || strings.HasSuffix(limit, "M") {
		fmt.Sscanf(limit, "%d", &value)
		return value * 1024 * 1024
	}
	fmt.Sscanf(limit, "%d", &value)
	return value
}

func splitDockerLogs(logs string) (stdout, stderr string) {
	// Docker multiplexed stream format: each line has 8-byte header
	// For simplicity, return all as stdout
	return logs, ""
}
