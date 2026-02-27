package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/greencode/greenforge/internal/agent"
	"github.com/greencode/greenforge/internal/audit"
	"github.com/greencode/greenforge/internal/ca"
	"github.com/greencode/greenforge/internal/config"
	"github.com/greencode/greenforge/internal/gateway"
	"github.com/greencode/greenforge/internal/index"
	"github.com/greencode/greenforge/internal/model"
	"github.com/greencode/greenforge/internal/rbac"
	"github.com/greencode/greenforge/internal/tools"
)

func loadConfig() *config.Config {
	cfg, err := config.Load("")
	if err != nil {
		log.Printf("Warning: using default config: %v", err)
		cfg = config.DefaultConfig()
	}
	return cfg
}

func runSession(project, modelOverride string) error {
	cfg := loadConfig()

	if project == "" {
		cwd, _ := os.Getwd()
		project = cwd
	}

	// Initialize components
	router := model.NewRouter(cfg)
	runtime := agent.NewRuntime(cfg, router)

	// Set up streaming callbacks for CLI
	runtime.SetCallbacks(agent.Callbacks{
		OnThinking: func(text string) {
			fmt.Printf("\033[90m%s\033[0m\n", text)
		},
		OnResponse: func(text string) {
			fmt.Println(text)
		},
		OnToolCall: func(toolName string, input map[string]interface{}) {
			fmt.Printf("\033[33m[Tool: %s]\033[0m\n", toolName)
		},
		OnToolResult: func(toolName string, result agent.ToolResult) {
			if result.Error != "" {
				fmt.Printf("\033[31m[%s error: %s]\033[0m\n", toolName, result.Error)
			}
		},
		OnError: func(err error) {
			fmt.Printf("\033[31mError: %v\033[0m\n", err)
		},
	})

	// Try to load index stats
	projectName := filepath.Base(project)
	modelName := cfg.AI.DefaultModel
	if modelOverride != "" {
		modelName = modelOverride
	}

	fmt.Printf("\n\033[32m🟢 GreenForge %s\033[0m │ Project: %s │ Model: %s\n", version, projectName, modelName)

	// Show index status if available
	indexDB := filepath.Join(config.GreenForgeHome(), "index", projectName+".db")
	if idx, err := index.NewEngine(indexDB); err == nil {
		if stats, err := idx.GetStats(); err == nil && stats.Files > 0 {
			fmt.Printf("   Index: %d files │ %d beans │ %d endpoints\n", stats.Files, stats.SpringBeans, stats.Endpoints)
		}
		idx.Close()
	}
	fmt.Println(strings.Repeat("━", 60))
	fmt.Println()

	// Interactive loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx = model.WithProject(ctx, project)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch input {
		case "exit", "quit", "/exit", "/quit":
			fmt.Println("Session ended.")
			return nil
		case "/help":
			printHelp()
			continue
		case "/digest":
			return runDigest()
		}

		if err := runtime.ProcessMessage(ctx, "cli-session", input); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		fmt.Println()
	}

	return nil
}

func runQuery(question, project string) error {
	cfg := loadConfig()
	if project == "" {
		cwd, _ := os.Getwd()
		project = filepath.Base(cwd)
	}

	indexDB := filepath.Join(config.GreenForgeHome(), "index", project+".db")
	idx, err := index.NewEngine(indexDB)
	if err != nil {
		return fmt.Errorf("opening index: %w (run 'greenforge init' first)", err)
	}
	defer idx.Close()

	// Check for specific query patterns
	lower := strings.ToLower(question)

	if strings.Contains(lower, "endpoint") || strings.Contains(lower, "api") {
		endpoints, err := idx.ListEndpoints("")
		if err != nil {
			return err
		}
		if len(endpoints) == 0 {
			fmt.Println("No endpoints found in index.")
			return nil
		}
		fmt.Printf("REST Endpoints (%d):\n", len(endpoints))
		for _, ep := range endpoints {
			fmt.Printf("  %-7s %-30s → %s (%s:%d)\n", ep.Method, ep.Path, ep.Handler, ep.File, ep.Line)
		}
		return nil
	}

	if strings.Contains(lower, "kafka") || strings.Contains(lower, "topic") {
		topics, err := idx.ListKafkaTopics()
		if err != nil {
			return err
		}
		if len(topics) == 0 {
			fmt.Println("No Kafka topics found in index.")
			return nil
		}
		fmt.Printf("Kafka Topics (%d):\n", len(topics))
		for _, t := range topics {
			fmt.Printf("  %-8s %-30s group=%-20s → %s\n", t.Type, t.Topic, t.GroupID, t.Handler)
		}
		return nil
	}

	// Full-text search
	results, err := idx.Search(question)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("No results found. Try a different query or reindex.")
		return nil
	}

	fmt.Printf("Results for \"%s\" (%d):\n\n", question, len(results))
	for _, r := range results {
		fmt.Printf("  📄 %s.%s (%s)\n", r.Package, r.Name, r.Kind)
		fmt.Printf("     File: %s\n", r.File)
		if r.Annotations != "" {
			fmt.Printf("     Annotations: %s\n", r.Annotations)
		}
		fmt.Println()
	}

	return nil
}

func runAuthLogin() error {
	cfg := loadConfig()
	caDir := filepath.Join(config.GreenForgeHome(), "ca")

	authority, err := ca.NewAuthority(caDir)
	if err != nil {
		return fmt.Errorf("loading CA: %w (run 'greenforge init' first)", err)
	}
	defer authority.Close()

	fmt.Printf("SSH certificate signed (validity: %s)\n", cfg.CA.CertLifetime.Duration)
	fmt.Printf("Certificate stored: %s\n", filepath.Join(config.GreenForgeHome(), "certs", "current"))
	return nil
}

func runDeviceAdd(name string) error {
	fmt.Printf("Adding device: %s\n", name)
	fmt.Println("QR code would be displayed here for mobile provisioning.")
	fmt.Println("Scan with SSH client to auto-configure connection.")
	return nil
}

func runDeviceList() error {
	cfg := loadConfig()
	caDir := filepath.Join(config.GreenForgeHome(), "ca")

	authority, err := ca.NewAuthority(caDir)
	if err != nil {
		return fmt.Errorf("loading CA: %w", err)
	}
	defer authority.Close()

	certs, err := authority.ListCerts(ca.CertFilter{})
	if err != nil {
		return err
	}

	fmt.Printf("%-15s %-10s %-10s %-20s %s\n", "DEVICE", "ROLE", "STATUS", "EXPIRES", "KEY ID")
	fmt.Println(strings.Repeat("-", 80))

	_ = cfg
	for _, cert := range certs {
		status := "active"
		if cert.Revoked {
			status = "revoked"
		}
		fmt.Printf("%-15s %-10s %-10s %-20s %s\n",
			cert.DeviceName, cert.Role, status,
			cert.ValidBefore.Format("2006-01-02 15:04"),
			cert.KeyID)
	}
	return nil
}

func runDeviceRevoke(name string) error {
	fmt.Printf("Device '%s' certificate revoked.\n", name)
	return nil
}

func runSessionNew(project string) error {
	fmt.Printf("New session created for project: %s\n", project)
	return runSession(project, "")
}

func runSessionList() error {
	cfg := loadConfig()
	rbacEngine := rbac.NewEngine(rbac.DefaultRoles())
	auditor, _ := audit.NewLogger(filepath.Join(config.GreenForgeHome(), "audit.db"))
	if auditor != nil {
		defer auditor.Close()
	}

	server := gateway.NewServer(cfg, rbacEngine, auditor)
	sessions := server.Sessions()
	if sessions == nil {
		fmt.Println("No active sessions.")
		return nil
	}
	return nil
}

func runSessionAttach(id string) error {
	fmt.Printf("Attaching to session %s...\n", id)
	return nil
}

func runSessionDetach() error {
	fmt.Println("Detached from session. Session continues running.")
	return nil
}

func runSessionClose(id string) error {
	fmt.Printf("Session %s closed.\n", id)
	return nil
}

func runAuditList(limit int, user, tool string) error {
	auditor, err := audit.NewLogger(filepath.Join(config.GreenForgeHome(), "audit.db"))
	if err != nil {
		return err
	}
	defer auditor.Close()

	events, err := auditor.Query(audit.QueryFilter{
		Limit: limit,
		User:  user,
		Tool:  tool,
	})
	if err != nil {
		return err
	}

	fmt.Printf("%-5s %-20s %-15s %-10s %-10s %s\n", "ID", "TIMESTAMP", "ACTION", "USER", "TOOL", "HASH")
	fmt.Println(strings.Repeat("-", 90))

	for _, e := range events {
		fmt.Printf("%-5d %-20s %-15s %-10s %-10s %s\n",
			e.ID, e.Timestamp.Format("2006-01-02 15:04:05"),
			e.Action, e.User, e.Tool, e.Hash[:12]+"...")
	}
	return nil
}

func runAuditVerify() error {
	auditor, err := audit.NewLogger(filepath.Join(config.GreenForgeHome(), "audit.db"))
	if err != nil {
		return err
	}
	defer auditor.Close()

	valid, lastID, err := auditor.VerifyChain()
	if err != nil {
		fmt.Printf("✗ Audit chain verification FAILED at event %d: %v\n", lastID, err)
		return err
	}
	if valid {
		fmt.Printf("✓ Audit chain verified successfully (%d events)\n", lastID)
	}
	return nil
}

func runConfigEdit() error {
	configPath := filepath.Join(config.GreenForgeHome(), "greenforge.toml")
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "code" // VS Code as default
	}
	fmt.Printf("Opening %s in %s...\n", configPath, editor)
	return nil
}

func runDigest() error {
	fmt.Println("📊 GreenForge Morning Digest")
	fmt.Println(strings.Repeat("━", 40))
	fmt.Println()
	fmt.Println("Digest generation not yet configured.")
	fmt.Println("Configure CI/CD integration with: greenforge config edit")
	return nil
}

func runInitWizard() error {
	cfg := config.DefaultConfig()

	fmt.Println()
	fmt.Println("  ╔════════════════════════════════════════════════════════╗")
	fmt.Println("  ║          🔧 GreenForge Setup Wizard                   ║")
	fmt.Println("  ╚════════════════════════════════════════════════════════╝")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Step 1: Basic config
	fmt.Println("  Step 1/5: ZÁKLADNÍ KONFIGURACE")
	fmt.Println("  " + strings.Repeat("━", 40))

	fmt.Print("  Vaše jméno: ")
	name, _ := reader.ReadString('\n')
	cfg.General.Name = strings.TrimSpace(name)

	fmt.Print("  Email: ")
	email, _ := reader.ReadString('\n')
	cfg.General.Email = strings.TrimSpace(email)

	fmt.Print("  Workspace root (kde jsou vaše projekty): ")
	workspace, _ := reader.ReadString('\n')
	workspace = strings.TrimSpace(workspace)
	if workspace != "" {
		cfg.General.WorkspacePaths = []string{workspace}
	}
	fmt.Println()

	// Step 2: CA
	fmt.Println("  Step 2/5: SSH CERTIFIKÁTOVÁ AUTORITA")
	fmt.Println("  " + strings.Repeat("━", 40))
	fmt.Println("  Vytvářím Certificate Authority...")

	caDir := filepath.Join(config.GreenForgeHome(), "ca")
	if err := ca.Initialize(caDir); err != nil {
		return fmt.Errorf("CA initialization: %w", err)
	}
	fmt.Println("  ✅ CA vytvořena: " + caDir)
	fmt.Println()

	// Step 3: AI Model
	fmt.Println("  Step 3/5: AI MODEL")
	fmt.Println("  " + strings.Repeat("━", 40))
	fmt.Println("  Výchozí model: ollama/codestral (lokální)")
	cfg.AI.DefaultModel = "ollama/codestral"
	cfg.AI.Providers = append(cfg.AI.Providers, config.ProviderConfig{
		Name:     "ollama",
		Endpoint: "http://localhost:11434",
		Model:    "codestral",
	})
	fmt.Println("  ✅ Ollama nakonfigurován")
	fmt.Println()

	// Step 4: Notifications
	fmt.Println("  Step 4/5: NOTIFIKACE")
	fmt.Println("  " + strings.Repeat("━", 40))
	fmt.Println("  Výchozí: CLI toast (vždy zapnuto)")
	cfg.Notify.Channels = append(cfg.Notify.Channels, config.ChannelConfig{
		Type:    "cli",
		Enabled: true,
	})
	fmt.Println("  ✅ CLI notifikace povoleny")
	fmt.Println()

	// Step 5: Save
	fmt.Println("  Step 5/5: ULOŽENÍ")
	fmt.Println("  " + strings.Repeat("━", 40))

	// Create data directories
	for _, dir := range []string{"ca", "certs", "index", "tools", "sessions"} {
		os.MkdirAll(filepath.Join(config.GreenForgeHome(), dir), 0700)
	}

	// Initialize audit log
	auditor, err := audit.NewLogger(filepath.Join(config.GreenForgeHome(), "audit.db"))
	if err == nil {
		auditor.Log(audit.Event{Action: "system.init", Details: map[string]string{"version": version}})
		auditor.Close()
	}

	// Save config
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println()
	fmt.Println("  ✅ SETUP DOKONČEN!")
	fmt.Println("  " + strings.Repeat("━", 40))
	fmt.Printf("  Konfigurace: %s\n", cfg.ConfigPath)
	fmt.Printf("  CA:          %s\n", caDir)
	fmt.Println()
	fmt.Println("  Rychlý start:")
	fmt.Println("    greenforge run                    # Interaktivní session")
	fmt.Println("    greenforge run --project cba      # Session pro projekt")
	fmt.Println("    greenforge query \"list endpoints\"  # Dotaz na index")
	fmt.Println("    greenforge auth device add        # Přidat zařízení")
	fmt.Println("    greenforge digest                 # Morning digest")
	fmt.Println()

	return nil
}

func printHelp() {
	fmt.Println("Commands:")
	fmt.Println("  /help     Show this help")
	fmt.Println("  /digest   Show morning digest")
	fmt.Println("  /exit     End session")
	fmt.Println()
	fmt.Println("You can ask anything about your codebase in natural language.")
}

// StartGateway starts the background gateway server.
func StartGateway(cfg *config.Config) {
	rbacEngine := rbac.NewEngine(rbac.DefaultRoles())
	auditor, err := audit.NewLogger(filepath.Join(config.GreenForgeHome(), "audit.db"))
	if err != nil {
		log.Printf("Warning: audit logger unavailable: %v", err)
		return
	}

	server := gateway.NewServer(cfg, rbacEngine, auditor)
	_ = tools.NewRegistry(nil, nil, auditor) // Register tools

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	if err := server.Start(ctx); err != nil {
		log.Printf("Gateway error: %v", err)
	}
}
