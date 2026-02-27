package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/greencode/greenforge/internal/config"
	"github.com/spf13/cobra"
)

//go:embed web
var embeddedWeb embed.FS

// webFS is the filesystem for the web UI, extracted from embed.
var webFS fs.FS

var (
	version = "0.1.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

func init() {
	var err error
	webFS, err = fs.Sub(embeddedWeb, "web")
	if err != nil {
		webFS = nil
	}
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "greenforge",
		Short: "Secure AI developer agent for JVM teams",
		Long: `GreenForge - Secure AI Developer Agent pro JVM Týmy

Rozumí vašemu Spring Boot projektu, hlídá pipeline,
a pomáhá z terminálu i z mobilu.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
	}

	// Global flags
	rootCmd.PersistentFlags().StringP("config", "c", "", "config file path (default: ~/.greenforge/greenforge.toml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "enable verbose output")

	// Register all commands
	rootCmd.AddCommand(
		newInitCmd(),
		newRunCmd(),
		newServeCmd(),
		newQueryCmd(),
		newIndexCmd(),
		newAuthCmd(),
		newSessionCmd(),
		newAuditCmd(),
		newConfigCmd(),
		newDigestCmd(),
		newVersionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// newInitCmd creates the `greenforge init` command - interactive setup wizard
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Interactive setup wizard for GreenForge",
		Long:  "Provede vás celým setupem: CA, AI model, Docker sandbox, notifikace, codebase index.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitWizard()
		},
	}
	return cmd
}

// newRunCmd creates the `greenforge run` command - interactive AI session
func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start interactive AI agent session",
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			model, _ := cmd.Flags().GetString("model")
			return runSession(project, model)
		},
	}
	cmd.Flags().StringP("project", "p", "", "project path or name")
	cmd.Flags().StringP("model", "m", "", "AI model override (e.g. ollama/codestral)")
	return cmd
}

// newQueryCmd creates the `greenforge query` command - codebase queries
func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query [question]",
		Short: "Query the codebase index",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			return runQuery(args[0], project)
		},
	}
	cmd.Flags().StringP("project", "p", "", "project to query")
	return cmd
}

// newIndexCmd creates the `greenforge index` command - indexes project codebase
func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index [project-path]",
		Short: "Index a project codebase for AI context",
		Long:  "Indexuje Java/Kotlin projekt - třídy, Spring beany, endpointy, Kafka topiky, JPA entity. Data se uloží do SQLite a AI agent je pak může využívat.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := "."
			if len(args) > 0 {
				projectPath = args[0]
			}
			incremental, _ := cmd.Flags().GetBool("incremental")
			return runIndex(projectPath, incremental)
		},
	}
	cmd.Flags().BoolP("incremental", "i", false, "only re-index changed files (git diff)")
	return cmd
}

// newAuthCmd creates the `greenforge auth` command tree
func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication and certificate management",
	}

	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Obtain a signed SSH certificate",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin()
		},
	}

	deviceCmd := &cobra.Command{
		Use:   "device",
		Short: "Device management",
	}

	deviceAddCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new device (generates QR code for mobile)",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			return runDeviceAdd(name)
		},
	}
	deviceAddCmd.Flags().String("name", "", "device name (e.g. 'iPhone')")
	deviceAddCmd.MarkFlagRequired("name")

	deviceListCmd := &cobra.Command{
		Use:   "list",
		Short: "List registered devices",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceList()
		},
	}

	deviceRevokeCmd := &cobra.Command{
		Use:   "revoke [device-name]",
		Short: "Revoke device certificate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceRevoke(args[0])
		},
	}

	deviceCmd.AddCommand(deviceAddCmd, deviceListCmd, deviceRevokeCmd)

	devicesCmd := &cobra.Command{
		Use:   "devices",
		Short: "List all devices (alias for 'auth device list')",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceList()
		},
	}

	cmd.AddCommand(loginCmd, deviceCmd, devicesCmd)
	return cmd
}

// newSessionCmd creates the `greenforge session` command tree
func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage AI sessions (tmux-style)",
	}

	newCmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new session",
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			return runSessionNew(project)
		},
	}
	newCmd.Flags().StringP("project", "p", "", "project for session")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List active sessions",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionList()
		},
	}

	attachCmd := &cobra.Command{
		Use:   "attach [session-id]",
		Short: "Attach to existing session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionAttach(args[0])
		},
	}

	detachCmd := &cobra.Command{
		Use:   "detach",
		Short: "Detach from current session (session keeps running)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionDetach()
		},
	}

	closeCmd := &cobra.Command{
		Use:   "close [session-id]",
		Short: "Close and terminate a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionClose(args[0])
		},
	}

	cmd.AddCommand(newCmd, listCmd, attachCmd, detachCmd, closeCmd)
	return cmd
}

// newAuditCmd creates the `greenforge audit` command
func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "View audit log",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List audit events",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, _ := cmd.Flags().GetInt("limit")
			user, _ := cmd.Flags().GetString("user")
			tool, _ := cmd.Flags().GetString("tool")
			return runAuditList(limit, user, tool)
		},
	}
	listCmd.Flags().IntP("limit", "n", 50, "max entries to show")
	listCmd.Flags().String("user", "", "filter by user")
	listCmd.Flags().String("tool", "", "filter by tool")

	verifyCmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify audit log hash chain integrity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuditVerify()
		},
	}

	cmd.AddCommand(listCmd, verifyCmd)
	return cmd
}

// newConfigCmd creates the `greenforge config` command
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
	}

	editCmd := &cobra.Command{
		Use:   "edit",
		Short: "Open config in editor",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigEdit()
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			fmt.Printf("Config file: %s\n", cfg.ConfigPath)
			return config.Print(cfg)
		},
	}

	cmd.AddCommand(editCmd, showCmd)
	return cmd
}

// newServeCmd creates the `greenforge serve` command - runs gateway server (for Docker)
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start GreenForge gateway server (used in Docker)",
		Long:  "Spustí gateway server na pozadí a čeká. Určeno pro běh v Docker kontejneru.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe()
		},
	}
	return cmd
}

// newDigestCmd creates the `greenforge digest` command
func newDigestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "digest",
		Short: "Show morning digest (pipeline status, PRs, commits)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDigest()
		},
	}
}

// newVersionCmd shows extended version info
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("GreenForge %s\n", version)
			fmt.Printf("  Commit:  %s\n", commit)
			fmt.Printf("  Built:   %s\n", date)
		},
	}
}
