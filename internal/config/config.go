package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration for GreenForge.
type Config struct {
	ConfigPath string `toml:"-"` // path to the loaded config file

	General  GeneralConfig  `toml:"general"`
	CA       CAConfig       `toml:"ca"`
	AI       AIConfig       `toml:"ai"`
	Sandbox  SandboxConfig  `toml:"sandbox"`
	Notify   NotifyConfig   `toml:"notify"`
	CICD     CICDConfig     `toml:"cicd"`
	Index    IndexConfig    `toml:"index"`
	Gateway  GatewayConfig  `toml:"gateway"`
	Audit    AuditConfig    `toml:"audit"`
	AutoFix  AutoFixConfig  `toml:"autofix"`
	Projects []ProjectEntry `toml:"projects"`
}

type GeneralConfig struct {
	Name           string   `toml:"name"`
	Email          string   `toml:"email"`
	WorkspacePaths []string `toml:"workspace_paths"`
	LogLevel       string   `toml:"log_level"`
	Language       string   `toml:"language"`
	DataDir        string   `toml:"data_dir"`
}

type CAConfig struct {
	CertLifetime        Duration `toml:"cert_lifetime"`
	AutoRenewThreshold  float64  `toml:"auto_renew_threshold"` // percentage, e.g. 0.20
	Algo                string   `toml:"algo"`
	DeviceCertLifetime  Duration `toml:"device_cert_lifetime"`
	MaxDevicesPerUser   int      `toml:"max_devices_per_user"`
	PermissionsMode     string   `toml:"permissions_mode"`
	AllowedDeviceTools  []string `toml:"allowed_device_tools"`
}

type AIConfig struct {
	DefaultModel string           `toml:"default_model"`
	Providers    []ProviderConfig `toml:"providers"`
	Policies     []ModelPolicy    `toml:"policies"`
}

type ProviderConfig struct {
	Name     string `toml:"name"`     // anthropic, openai, ollama
	Endpoint string `toml:"endpoint"` // URL
	APIKey   string `toml:"api_key"`  // keychain reference, not plaintext
	Model    string `toml:"model"`    // default model for this provider
}

type ModelPolicy struct {
	ProjectPattern   string   `toml:"project_pattern"`
	AllowedProviders []string `toml:"allowed_providers"`
	Reason           string   `toml:"reason"`
}

type SandboxConfig struct {
	Enabled      bool   `toml:"enabled"`
	DockerSocket string `toml:"docker_socket"`
	NetworkMode  string `toml:"network_mode"`
	CPULimit     string `toml:"cpu_limit"`
	MemoryLimit  string `toml:"memory_limit"`
	Timeout      Duration `toml:"timeout"`
}

type NotifyConfig struct {
	Channels      []ChannelConfig `toml:"channels"`
	Events        EventsConfig    `toml:"events"`
	MorningDigest DigestConfig    `toml:"morning_digest"`
	QuietHours    QuietHours      `toml:"quiet_hours"`
}

type ChannelConfig struct {
	Type    string `toml:"type"` // email, telegram, whatsapp, sms, cli
	Enabled bool   `toml:"enabled"`
	// Channel-specific fields
	Address  string `toml:"address,omitempty"`   // email address
	BotToken string `toml:"bot_token,omitempty"` // telegram bot token (keychain ref)
	ChatID   string `toml:"chat_id,omitempty"`   // telegram chat ID
	Phone    string `toml:"phone,omitempty"`     // whatsapp/sms number
}

type EventsConfig struct {
	PipelineFailures bool `toml:"pipeline_failures"`
	PRAssigned       bool `toml:"pr_assigned"`
	AllCommits       bool `toml:"all_commits"`
	AutoFixCompleted bool `toml:"autofix_completed"`
}

type DigestConfig struct {
	Mode string `toml:"mode"` // automatic, on_demand, both
	Time string `toml:"time"` // HH:MM for automatic mode
}

type QuietHours struct {
	Enabled bool   `toml:"enabled"`
	Start   string `toml:"start"` // HH:MM
	End     string `toml:"end"`   // HH:MM
}

type CICDConfig struct {
	AzureDevOps *AzureDevOpsConfig `toml:"azure_devops,omitempty"`
	GitLab      *GitLabConfig      `toml:"gitlab,omitempty"`
	GitHub      *GitHubConfig      `toml:"github,omitempty"`
}

type AzureDevOpsConfig struct {
	Organization string `toml:"organization"`
	PATToken     string `toml:"pat_token"` // keychain reference
}

type GitLabConfig struct {
	URL   string `toml:"url"`
	Token string `toml:"token"` // keychain reference
}

type GitHubConfig struct {
	Token string `toml:"token"` // keychain reference
}

type IndexConfig struct {
	Enabled         bool   `toml:"enabled"`
	BackgroundWatch bool   `toml:"background_watch"`
	EmbeddingModel  string `toml:"embedding_model"`
}

type GatewayConfig struct {
	Host      string   `toml:"host"`
	Port      int      `toml:"port"`
	WebUIPort int      `toml:"webui_port"`
	TLS       bool     `toml:"tls"`
	CertFile  string   `toml:"cert_file"`
	KeyFile   string   `toml:"key_file"`
}

type AuditConfig struct {
	Enabled    bool   `toml:"enabled"`
	DBPath     string `toml:"db_path"`
	RetainDays int    `toml:"retain_days"`
}

type AutoFixConfig struct {
	DefaultPolicy string            `toml:"default_policy"` // notify_only, fix_and_pr, fix_and_merge
	MaxAutoFixes  int               `toml:"max_auto_fixes"`
	EscalateAfter Duration          `toml:"escalate_after"`
	RepoPolicies  []RepoFixPolicy   `toml:"repo_policies"`
}

type RepoFixPolicy struct {
	Repo    string           `toml:"repo"`
	Rules   []BranchFixRule  `toml:"rules"`
}

type BranchFixRule struct {
	Branch         string   `toml:"branch"`
	OnFailure      string   `toml:"on_failure"`
	PRAssignee     string   `toml:"pr_assignee,omitempty"`
	RequireReview  bool     `toml:"require_review"`
	RequireTests   bool     `toml:"require_tests_pass"`
	MaxAutoFixes   int      `toml:"max_auto_fixes"`
	EscalateAfter  Duration `toml:"escalate_after,omitempty"`
	NotifyChannels []string `toml:"notify,omitempty"`
}

type ProjectEntry struct {
	Name      string `toml:"name"`
	Path      string `toml:"path"`
	BuildTool string `toml:"build_tool"` // gradle, maven
	CICD      string `toml:"cicd"`       // azdo, gitlab, github
}

// Duration wraps time.Duration for TOML serialization.
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	homeDir := greenforgeHome()
	return &Config{
		General: GeneralConfig{
			LogLevel: "info",
			Language: "cs",
			DataDir:  homeDir,
		},
		CA: CAConfig{
			CertLifetime:       Duration{8 * time.Hour},
			AutoRenewThreshold: 0.20,
			Algo:               "ed25519",
			DeviceCertLifetime: Duration{30 * 24 * time.Hour},
			MaxDevicesPerUser:  5,
			PermissionsMode:    "restricted",
			AllowedDeviceTools: []string{"git:read", "logs:read", "audit:read", "notify:send"},
		},
		AI: AIConfig{
			DefaultModel: "ollama/codestral",
		},
		Sandbox: SandboxConfig{
			Enabled:     true,
			NetworkMode: "restricted",
			CPULimit:    "2.0",
			MemoryLimit: "2048m",
			Timeout:     Duration{5 * time.Minute},
		},
		Notify: NotifyConfig{
			Events: EventsConfig{
				PipelineFailures: true,
				PRAssigned:       true,
			},
			MorningDigest: DigestConfig{
				Mode: "on_demand",
				Time: "07:30",
			},
			QuietHours: QuietHours{
				Enabled: true,
				Start:   "22:00",
				End:     "07:00",
			},
		},
		Gateway: GatewayConfig{
			Host:      "127.0.0.1",
			Port:      18788,
			WebUIPort: 18789,
		},
		Audit: AuditConfig{
			Enabled:    true,
			RetainDays: 90,
		},
		AutoFix: AutoFixConfig{
			DefaultPolicy: "notify_only",
			MaxAutoFixes:  3,
			EscalateAfter: Duration{30 * time.Minute},
		},
		Index: IndexConfig{
			Enabled:         true,
			BackgroundWatch: true,
		},
	}
}

// Load reads config from file path. If path is empty, uses default location.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		path = filepath.Join(greenforgeHome(), "greenforge.toml")
	}
	cfg.ConfigPath = path

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			return cfg, nil // use defaults
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// Save writes config to file.
func Save(cfg *Config) error {
	path := cfg.ConfigPath
	if path == "" {
		path = filepath.Join(greenforgeHome(), "greenforge.toml")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	return encoder.Encode(cfg)
}

// Print outputs the config to stdout.
func Print(cfg *Config) error {
	return toml.NewEncoder(os.Stdout).Encode(cfg)
}

// greenforgeHome returns the GreenForge data directory.
func greenforgeHome() string {
	if dir := os.Getenv("GREENFORGE_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if runtime.GOOS == "windows" {
			return filepath.Join("C:", "Users", os.Getenv("USERNAME"), ".greenforge")
		}
		return filepath.Join("/home", os.Getenv("USER"), ".greenforge")
	}
	return filepath.Join(home, ".greenforge")
}

// applyEnvOverrides applies environment variable overrides to config.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("GF_GATEWAY_HOST"); v != "" {
		cfg.Gateway.Host = v
	}
	if v := os.Getenv("GF_GATEWAY_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil {
			cfg.Gateway.Port = port
		}
	}
	if v := os.Getenv("GF_WEBUI_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil {
			cfg.Gateway.WebUIPort = port
		}
	}
	if v := os.Getenv("GF_DEFAULT_MODEL"); v != "" {
		cfg.AI.DefaultModel = v
	}
	if v := os.Getenv("OLLAMA_HOST"); v != "" {
		// Update or add Ollama provider endpoint
		found := false
		for i := range cfg.AI.Providers {
			if cfg.AI.Providers[i].Name == "ollama" {
				cfg.AI.Providers[i].Endpoint = v
				found = true
				break
			}
		}
		if !found && len(cfg.AI.Providers) == 0 {
			cfg.AI.Providers = append(cfg.AI.Providers, ProviderConfig{
				Name:     "ollama",
				Endpoint: v,
				Model:    "codestral",
			})
		}
	}
}

// GreenForgeHome is exported for use by other packages.
func GreenForgeHome() string {
	return greenforgeHome()
}
