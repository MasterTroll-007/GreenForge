package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/greencode/greenforge/internal/config"
	"github.com/greencode/greenforge/internal/model"
)

// maskSecret returns "••••••" if the string is non-empty, otherwise "".
func maskSecret(s string) string {
	if s != "" {
		return "••••••"
	}
	return ""
}

// WebUIServer serves the embedded web UI.
type WebUIServer struct {
	gateway *Server
	router  *model.Router
	webFS   fs.FS // embedded filesystem from caller
}

// NewWebUIServer creates a web UI server.
func NewWebUIServer(gateway *Server, router *model.Router, webFS fs.FS) *WebUIServer {
	return &WebUIServer{
		gateway: gateway,
		router:  router,
		webFS:   webFS,
	}
}

// Handler returns an http.Handler that serves the web UI and API.
func (w *WebUIServer) SetupRoutes(mux *http.ServeMux) {
	// API endpoints for the web UI
	mux.HandleFunc("/api/v1/models", w.handleModels)
	mux.HandleFunc("/api/v1/config", w.handleConfig)
	mux.HandleFunc("/api/v1/chat", w.handleChat)
	mux.HandleFunc("/api/v1/projects", w.handleProjects)
	mux.HandleFunc("/api/v1/workspace", w.handleWorkspace)
	mux.HandleFunc("/api/v1/browse", w.handleBrowse)
	mux.HandleFunc("/api/v1/digest", w.handleDigest)
	mux.HandleFunc("/api/v1/index/stats", w.handleIndexStats)
	mux.HandleFunc("/api/v1/index/reindex", w.handleReindex)
	mux.HandleFunc("/api/v1/watcher/status", w.handleWatcherStatus)

	// Serve embedded static files
	if w.webFS != nil {
		fileServer := http.FileServer(http.FS(w.webFS))
		mux.Handle("/", fileServer)
	} else {
		log.Printf("Warning: no embedded web UI filesystem provided")
	}
}

func (w *WebUIServer) handleModels(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		if w.router == nil {
			json.NewEncoder(rw).Encode(map[string]interface{}{
				"models":  []interface{}{},
				"default": "",
			})
			return
		}

		models := w.router.ListModels()
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"models":  models,
			"default": w.router.GetDefaultModel(),
		})

	case http.MethodPut:
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(rw, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if w.router != nil {
			if err := w.router.SetDefaultModel(req.Model); err != nil {
				http.Error(rw, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
		}
		json.NewEncoder(rw).Encode(map[string]string{"status": "ok", "model": req.Model})

	default:
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (w *WebUIServer) handleConfig(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	if w.gateway == nil || w.gateway.cfg == nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{})
		return
	}

	cfg := w.gateway.cfg

	switch r.Method {
	case http.MethodGet:
		w.handleConfigGET(rw, cfg)
	case http.MethodPut:
		w.handleConfigPUT(rw, r, cfg)
	default:
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (w *WebUIServer) handleConfigGET(rw http.ResponseWriter, cfg *config.Config) {
	// Build notify channels (mask secrets)
	var channels []map[string]interface{}
	for _, ch := range cfg.Notify.Channels {
		channels = append(channels, map[string]interface{}{
			"type":      ch.Type,
			"enabled":   ch.Enabled,
			"address":   ch.Address,
			"bot_token": maskSecret(ch.BotToken),
			"chat_id":   ch.ChatID,
			"phone":     ch.Phone,
		})
	}

	// Build autofix repo policies (full fields)
	var repoPolicies []map[string]interface{}
	for _, rp := range cfg.AutoFix.RepoPolicies {
		var rules []map[string]interface{}
		for _, rule := range rp.Rules {
			rules = append(rules, map[string]interface{}{
				"branch":          rule.Branch,
				"on_failure":      rule.OnFailure,
				"pr_assignee":     rule.PRAssignee,
				"require_review":  rule.RequireReview,
				"require_tests":   rule.RequireTests,
				"max_auto_fixes":  rule.MaxAutoFixes,
				"escalate_after":  rule.EscalateAfter.String(),
				"notify_channels": rule.NotifyChannels,
			})
		}
		repoPolicies = append(repoPolicies, map[string]interface{}{
			"repo":  rp.Repo,
			"rules": rules,
		})
	}

	// Build AI providers (mask API keys)
	var providers []map[string]interface{}
	for _, p := range cfg.AI.Providers {
		providers = append(providers, map[string]interface{}{
			"name":     p.Name,
			"endpoint": p.Endpoint,
			"api_key":  maskSecret(p.APIKey),
			"model":    p.Model,
		})
	}

	// Build AI policies
	var policies []map[string]interface{}
	for _, p := range cfg.AI.Policies {
		policies = append(policies, map[string]interface{}{
			"project_pattern":   p.ProjectPattern,
			"allowed_providers": p.AllowedProviders,
			"reason":            p.Reason,
		})
	}

	// Build CI/CD config (mask tokens)
	cicdCfg := map[string]interface{}{}
	if cfg.CICD.AzureDevOps != nil {
		cicdCfg["azure_devops"] = map[string]interface{}{
			"organization": cfg.CICD.AzureDevOps.Organization,
			"pat_token":    maskSecret(cfg.CICD.AzureDevOps.PATToken),
		}
	}
	if cfg.CICD.GitLab != nil {
		cicdCfg["gitlab"] = map[string]interface{}{
			"url":   cfg.CICD.GitLab.URL,
			"token": maskSecret(cfg.CICD.GitLab.Token),
		}
	}
	if cfg.CICD.GitHub != nil {
		cicdCfg["github"] = map[string]interface{}{
			"token": maskSecret(cfg.CICD.GitHub.Token),
		}
	}

	// Build projects list
	var projects []map[string]interface{}
	for _, p := range cfg.Projects {
		projects = append(projects, map[string]interface{}{
			"name":       p.Name,
			"path":       p.Path,
			"build_tool": p.BuildTool,
			"cicd":       p.CICD,
		})
	}

	json.NewEncoder(rw).Encode(map[string]interface{}{
		"general": map[string]interface{}{
			"name":     cfg.General.Name,
			"email":    cfg.General.Email,
			"log_level": cfg.General.LogLevel,
			"language": cfg.General.Language,
			"data_dir": cfg.General.DataDir,
		},
		"gateway": map[string]interface{}{
			"host":       cfg.Gateway.Host,
			"port":       cfg.Gateway.Port,
			"webui_port": cfg.Gateway.WebUIPort,
			"tls":        cfg.Gateway.TLS,
			"cert_file":  cfg.Gateway.CertFile,
			"key_file":   cfg.Gateway.KeyFile,
		},
		"ca": map[string]interface{}{
			"cert_lifetime":        cfg.CA.CertLifetime.String(),
			"auto_renew_threshold": cfg.CA.AutoRenewThreshold,
			"algo":                 cfg.CA.Algo,
			"device_cert_lifetime": cfg.CA.DeviceCertLifetime.String(),
			"max_devices_per_user": cfg.CA.MaxDevicesPerUser,
			"permissions_mode":     cfg.CA.PermissionsMode,
			"allowed_device_tools": cfg.CA.AllowedDeviceTools,
		},
		"ai": map[string]interface{}{
			"default_model": cfg.AI.DefaultModel,
			"providers":     providers,
			"policies":      policies,
		},
		"sandbox": map[string]interface{}{
			"enabled":       cfg.Sandbox.Enabled,
			"docker_socket": cfg.Sandbox.DockerSocket,
			"network_mode":  cfg.Sandbox.NetworkMode,
			"cpu":           cfg.Sandbox.CPULimit,
			"memory":        cfg.Sandbox.MemoryLimit,
			"timeout":       cfg.Sandbox.Timeout.String(),
		},
		"audit": map[string]interface{}{
			"enabled":     cfg.Audit.Enabled,
			"db_path":     cfg.Audit.DBPath,
			"retain_days": cfg.Audit.RetainDays,
		},
		"index": map[string]interface{}{
			"enabled":          cfg.Index.Enabled,
			"background_watch": cfg.Index.BackgroundWatch,
			"embedding_model":  cfg.Index.EmbeddingModel,
		},
		"notify": map[string]interface{}{
			"channels": channels,
			"events": map[string]interface{}{
				"pipeline_failures": cfg.Notify.Events.PipelineFailures,
				"pr_assigned":       cfg.Notify.Events.PRAssigned,
				"all_commits":       cfg.Notify.Events.AllCommits,
				"autofix_completed": cfg.Notify.Events.AutoFixCompleted,
			},
			"morning_digest": map[string]interface{}{
				"mode": cfg.Notify.MorningDigest.Mode,
				"time": cfg.Notify.MorningDigest.Time,
			},
			"quiet_hours": map[string]interface{}{
				"enabled": cfg.Notify.QuietHours.Enabled,
				"start":   cfg.Notify.QuietHours.Start,
				"end":     cfg.Notify.QuietHours.End,
			},
		},
		"autofix": map[string]interface{}{
			"default_policy": cfg.AutoFix.DefaultPolicy,
			"max_auto_fixes": cfg.AutoFix.MaxAutoFixes,
			"escalate_after": cfg.AutoFix.EscalateAfter.String(),
			"repo_policies":  repoPolicies,
		},
		"cicd":     cicdCfg,
		"projects": projects,
	})
}

func (w *WebUIServer) handleConfigPUT(rw http.ResponseWriter, r *http.Request, cfg *config.Config) {
	var req struct {
		Section string                 `json:"section"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Section == "" || req.Data == nil {
		http.Error(rw, `{"error":"section and data required"}`, http.StatusBadRequest)
		return
	}

	if err := w.applyConfigSection(cfg, req.Section, req.Data); err != nil {
		http.Error(rw, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	if err := config.Save(cfg); err != nil {
		log.Printf("config save error: %v", err)
		http.Error(rw, `{"error":"failed to save config: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(rw).Encode(map[string]string{"status": "ok"})
}

// helpers for reading map values
func strVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func boolVal(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func intVal(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

func float64Val(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func strSliceVal(m map[string]interface{}, key string) []string {
	if v, ok := m[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			var out []string
			for _, item := range arr {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return nil
}

func parseDuration(s string) config.Duration {
	if s == "" || s == "0s" {
		return config.Duration{}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return config.Duration{}
	}
	return config.Duration{Duration: d}
}

// secretOrKeep returns newVal unless it's the mask placeholder, in which case it keeps the old value.
func secretOrKeep(newVal, oldVal string) string {
	if newVal == "••••••" || newVal == "" {
		return oldVal
	}
	return newVal
}

func mapSlice(m map[string]interface{}, key string) []map[string]interface{} {
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []map[string]interface{}
	for _, item := range arr {
		if mm, ok := item.(map[string]interface{}); ok {
			out = append(out, mm)
		}
	}
	return out
}

func (w *WebUIServer) applyConfigSection(cfg *config.Config, section string, data map[string]interface{}) error {
	switch section {
	case "general":
		if v := strVal(data, "name"); v != "" {
			cfg.General.Name = v
		}
		if v := strVal(data, "email"); v != "" {
			cfg.General.Email = v
		}
		if v := strVal(data, "log_level"); v != "" {
			cfg.General.LogLevel = v
		}
		if v := strVal(data, "language"); v != "" {
			cfg.General.Language = v
		}
		if v := strVal(data, "data_dir"); v != "" {
			cfg.General.DataDir = v
		}

	case "gateway":
		if v := strVal(data, "host"); v != "" {
			cfg.Gateway.Host = v
		}
		if v, ok := data["port"]; ok {
			cfg.Gateway.Port = int(v.(float64))
		}
		if v, ok := data["webui_port"]; ok {
			cfg.Gateway.WebUIPort = int(v.(float64))
		}
		if v, ok := data["tls"]; ok {
			cfg.Gateway.TLS = v.(bool)
		}
		if v := strVal(data, "cert_file"); v != "" {
			cfg.Gateway.CertFile = v
		}
		if v := strVal(data, "key_file"); v != "" {
			cfg.Gateway.KeyFile = v
		}

	case "ca":
		if v := strVal(data, "cert_lifetime"); v != "" {
			cfg.CA.CertLifetime = parseDuration(v)
		}
		if v := float64Val(data, "auto_renew_threshold"); v > 0 {
			cfg.CA.AutoRenewThreshold = v
		}
		if v := strVal(data, "algo"); v != "" {
			cfg.CA.Algo = v
		}
		if v := strVal(data, "device_cert_lifetime"); v != "" {
			cfg.CA.DeviceCertLifetime = parseDuration(v)
		}
		if v := intVal(data, "max_devices_per_user"); v > 0 {
			cfg.CA.MaxDevicesPerUser = v
		}
		if v := strVal(data, "permissions_mode"); v != "" {
			cfg.CA.PermissionsMode = v
		}
		if v := strSliceVal(data, "allowed_device_tools"); v != nil {
			cfg.CA.AllowedDeviceTools = v
		}

	case "ai":
		if provs := mapSlice(data, "providers"); provs != nil {
			var newProviders []config.ProviderConfig
			for i, p := range provs {
				oldKey := ""
				if i < len(cfg.AI.Providers) {
					oldKey = cfg.AI.Providers[i].APIKey
				}
				newProviders = append(newProviders, config.ProviderConfig{
					Name:     strVal(p, "name"),
					Endpoint: strVal(p, "endpoint"),
					APIKey:   secretOrKeep(strVal(p, "api_key"), oldKey),
					Model:    strVal(p, "model"),
				})
			}
			cfg.AI.Providers = newProviders
		}
		if pols := mapSlice(data, "policies"); pols != nil {
			var newPolicies []config.ModelPolicy
			for _, p := range pols {
				newPolicies = append(newPolicies, config.ModelPolicy{
					ProjectPattern:   strVal(p, "project_pattern"),
					AllowedProviders: strSliceVal(p, "allowed_providers"),
					Reason:           strVal(p, "reason"),
				})
			}
			cfg.AI.Policies = newPolicies
		}

	case "sandbox":
		if _, ok := data["enabled"]; ok {
			cfg.Sandbox.Enabled = boolVal(data, "enabled")
		}
		if v := strVal(data, "docker_socket"); v != "" {
			cfg.Sandbox.DockerSocket = v
		}
		if v := strVal(data, "network_mode"); v != "" {
			cfg.Sandbox.NetworkMode = v
		}
		if v := strVal(data, "cpu"); v != "" {
			cfg.Sandbox.CPULimit = v
		}
		if v := strVal(data, "memory"); v != "" {
			cfg.Sandbox.MemoryLimit = v
		}
		if v := strVal(data, "timeout"); v != "" {
			cfg.Sandbox.Timeout = parseDuration(v)
		}

	case "notify":
		if chs := mapSlice(data, "channels"); chs != nil {
			var newChannels []config.ChannelConfig
			for i, ch := range chs {
				oldToken := ""
				if i < len(cfg.Notify.Channels) {
					oldToken = cfg.Notify.Channels[i].BotToken
				}
				newChannels = append(newChannels, config.ChannelConfig{
					Type:     strVal(ch, "type"),
					Enabled:  boolVal(ch, "enabled"),
					Address:  strVal(ch, "address"),
					BotToken: secretOrKeep(strVal(ch, "bot_token"), oldToken),
					ChatID:   strVal(ch, "chat_id"),
					Phone:    strVal(ch, "phone"),
				})
			}
			cfg.Notify.Channels = newChannels
		}
		if ev, ok := data["events"]; ok {
			if evMap, ok := ev.(map[string]interface{}); ok {
				cfg.Notify.Events.PipelineFailures = boolVal(evMap, "pipeline_failures")
				cfg.Notify.Events.PRAssigned = boolVal(evMap, "pr_assigned")
				cfg.Notify.Events.AllCommits = boolVal(evMap, "all_commits")
				cfg.Notify.Events.AutoFixCompleted = boolVal(evMap, "autofix_completed")
			}
		}
		if md, ok := data["morning_digest"]; ok {
			if mdMap, ok := md.(map[string]interface{}); ok {
				cfg.Notify.MorningDigest.Mode = strVal(mdMap, "mode")
				cfg.Notify.MorningDigest.Time = strVal(mdMap, "time")
			}
		}
		if qh, ok := data["quiet_hours"]; ok {
			if qhMap, ok := qh.(map[string]interface{}); ok {
				cfg.Notify.QuietHours.Enabled = boolVal(qhMap, "enabled")
				cfg.Notify.QuietHours.Start = strVal(qhMap, "start")
				cfg.Notify.QuietHours.End = strVal(qhMap, "end")
			}
		}

	case "cicd":
		if adoMap, ok := data["azure_devops"]; ok {
			if ado, ok := adoMap.(map[string]interface{}); ok {
				if cfg.CICD.AzureDevOps == nil {
					cfg.CICD.AzureDevOps = &config.AzureDevOpsConfig{}
				}
				if v := strVal(ado, "organization"); v != "" {
					cfg.CICD.AzureDevOps.Organization = v
				}
				cfg.CICD.AzureDevOps.PATToken = secretOrKeep(strVal(ado, "pat_token"), cfg.CICD.AzureDevOps.PATToken)
			}
		}
		if glMap, ok := data["gitlab"]; ok {
			if gl, ok := glMap.(map[string]interface{}); ok {
				if cfg.CICD.GitLab == nil {
					cfg.CICD.GitLab = &config.GitLabConfig{}
				}
				if v := strVal(gl, "url"); v != "" {
					cfg.CICD.GitLab.URL = v
				}
				cfg.CICD.GitLab.Token = secretOrKeep(strVal(gl, "token"), cfg.CICD.GitLab.Token)
			}
		}
		if ghMap, ok := data["github"]; ok {
			if gh, ok := ghMap.(map[string]interface{}); ok {
				if cfg.CICD.GitHub == nil {
					cfg.CICD.GitHub = &config.GitHubConfig{}
				}
				cfg.CICD.GitHub.Token = secretOrKeep(strVal(gh, "token"), cfg.CICD.GitHub.Token)
			}
		}

	case "index":
		if v, ok := data["enabled"]; ok {
			cfg.Index.Enabled = v.(bool)
		}
		if v, ok := data["background_watch"]; ok {
			cfg.Index.BackgroundWatch = v.(bool)
		}
		if v := strVal(data, "embedding_model"); v != "" {
			cfg.Index.EmbeddingModel = v
		}

	case "audit":
		if v, ok := data["enabled"]; ok {
			cfg.Audit.Enabled = v.(bool)
		}
		if v := strVal(data, "db_path"); v != "" {
			cfg.Audit.DBPath = v
		}
		if v := intVal(data, "retain_days"); v > 0 {
			cfg.Audit.RetainDays = v
		}

	case "autofix":
		if v := strVal(data, "default_policy"); v != "" {
			cfg.AutoFix.DefaultPolicy = v
		}
		if v := intVal(data, "max_auto_fixes"); v > 0 {
			cfg.AutoFix.MaxAutoFixes = v
		}
		if v := strVal(data, "escalate_after"); v != "" {
			cfg.AutoFix.EscalateAfter = parseDuration(v)
		}
		if rps := mapSlice(data, "repo_policies"); rps != nil {
			var newRPs []config.RepoFixPolicy
			for _, rp := range rps {
				var rules []config.BranchFixRule
				for _, rule := range mapSlice(rp, "rules") {
					rules = append(rules, config.BranchFixRule{
						Branch:         strVal(rule, "branch"),
						OnFailure:      strVal(rule, "on_failure"),
						PRAssignee:     strVal(rule, "pr_assignee"),
						RequireReview:  boolVal(rule, "require_review"),
						RequireTests:   boolVal(rule, "require_tests"),
						MaxAutoFixes:   intVal(rule, "max_auto_fixes"),
						EscalateAfter:  parseDuration(strVal(rule, "escalate_after")),
						NotifyChannels: strSliceVal(rule, "notify_channels"),
					})
				}
				newRPs = append(newRPs, config.RepoFixPolicy{
					Repo:  strVal(rp, "repo"),
					Rules: rules,
				})
			}
			cfg.AutoFix.RepoPolicies = newRPs
		}

	case "projects":
		if projs := mapSlice(data, "projects"); projs != nil {
			var newProjects []config.ProjectEntry
			for _, p := range projs {
				newProjects = append(newProjects, config.ProjectEntry{
					Name:      strVal(p, "name"),
					Path:      strVal(p, "path"),
					BuildTool: strVal(p, "build_tool"),
					CICD:      strVal(p, "cicd"),
				})
			}
			cfg.Projects = newProjects
		}

	default:
		return fmt.Errorf("unknown section: %s", section)
	}
	return nil
}

func (w *WebUIServer) handleWorkspace(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	// Proxy to host (which persists workspace paths on Windows)
	proxyURL := os.Getenv("ANTHROPIC_PROXY")
	if proxyURL != "" {
		targetURL := proxyURL + "/v1/workspace"
		var resp *http.Response
		var err error
		if r.Method == http.MethodGet {
			resp, err = http.Get(targetURL)
		} else if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			req, _ := http.NewRequest(http.MethodPut, targetURL, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err = http.DefaultClient.Do(req)
		} else {
			http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			rw.Write(body)
			return
		}
		// fallthrough to local if proxy fails
	}

	// Fallback: local in-memory workspace paths
	switch r.Method {
	case http.MethodGet:
		paths := []string{"/workspace"}
		if w.gateway != nil && w.gateway.cfg != nil {
			paths = w.gateway.cfg.General.WorkspacePaths
			if len(paths) == 0 {
				paths = []string{"/workspace"}
			}
		}
		json.NewEncoder(rw).Encode(map[string]interface{}{"paths": paths})
	case http.MethodPut:
		var req struct {
			Paths []string `json:"paths"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(rw, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if w.gateway != nil && w.gateway.cfg != nil && len(req.Paths) > 0 {
			w.gateway.cfg.General.WorkspacePaths = req.Paths
			// Persist to TOML so workspace paths survive restart
			if err := config.Save(w.gateway.cfg); err != nil {
				log.Printf("workspace config save error: %v", err)
			}
		}
		json.NewEncoder(rw).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (w *WebUIServer) handleProjects(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Proxy to host (which can see the real Windows filesystem)
	proxyURL := os.Getenv("ANTHROPIC_PROXY")
	if proxyURL != "" {
		targetURL := proxyURL + "/v1/projects"
		if q := r.URL.Query().Get("path"); q != "" {
			targetURL += "?path=" + q
		}
		resp, err := http.Get(targetURL)
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			rw.Write(body)
			return
		}
	}

	// Fallback: scan local Docker filesystem
	workspacePaths := []string{"/workspace"}
	type ProjectInfo struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		DockerPath string `json:"docker_path"`
		Git        bool   `json:"git"`
	}
	var projects []ProjectInfo
	seen := map[string]bool{}
	for _, wsPath := range workspacePaths {
		entries, err := os.ReadDir(wsPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name()[0] == '.' {
				continue
			}
			fullPath := filepath.Join(wsPath, entry.Name())
			if seen[entry.Name()] {
				continue
			}
			seen[entry.Name()] = true
			isGit := false
			if _, err := os.Stat(filepath.Join(fullPath, ".git")); err == nil {
				isGit = true
			}
			projects = append(projects, ProjectInfo{Name: entry.Name(), Path: fullPath, DockerPath: fullPath, Git: isGit})
		}
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	json.NewEncoder(rw).Encode(map[string]interface{}{"projects": projects, "workspaces": workspacePaths})
}

func (w *WebUIServer) handleBrowse(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Proxy browse request to host proxy (which can see the real Windows filesystem)
	proxyURL := os.Getenv("ANTHROPIC_PROXY")
	if proxyURL != "" {
		requestedPath := r.URL.Query().Get("path")
		targetURL := proxyURL + "/v1/browse"
		if requestedPath != "" {
			targetURL += "?path=" + requestedPath
		}
		resp, err := http.Get(targetURL)
		if err != nil {
			json.NewEncoder(rw).Encode(map[string]interface{}{
				"path":    requestedPath,
				"entries": []interface{}{},
				"error":   "Cannot reach host proxy: " + err.Error(),
			})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		rw.Write(body)
		return
	}

	// Fallback: browse local (Docker) filesystem
	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		requestedPath = "/"
	}
	requestedPath = filepath.Clean(requestedPath)

	type DirEntry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		IsGit bool   `json:"is_git"`
	}

	entries, err := os.ReadDir(requestedPath)
	if err != nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"path":    requestedPath,
			"parent":  filepath.Dir(requestedPath),
			"entries": []DirEntry{},
			"error":   err.Error(),
		})
		return
	}

	var dirs []DirEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name[0] == '.' {
			continue
		}
		fullPath := filepath.Join(requestedPath, name)
		isGit := false
		if _, err := os.Stat(filepath.Join(fullPath, ".git")); err == nil {
			isGit = true
		}
		dirs = append(dirs, DirEntry{
			Name:  name,
			Path:  fullPath,
			IsDir: true,
			IsGit: isGit,
		})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name < dirs[j].Name
	})

	json.NewEncoder(rw).Encode(map[string]interface{}{
		"path":    requestedPath,
		"parent":  filepath.Dir(requestedPath),
		"entries": dirs,
	})
}

func (w *WebUIServer) handleChat(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message  string   `json:"message"`
		Model    string   `json:"model"`
		Projects []string `json:"projects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if w.router == nil {
		json.NewEncoder(rw).Encode(map[string]string{"error": "no AI router configured"})
		return
	}

	// Build system prompt with index context
	systemPrompt := "You are GreenForge, an AI developer assistant for JVM teams. Be concise and helpful. Respond in the same language as the user.\n"
	if w.gateway != nil {
		systemPrompt += w.gateway.getIndexContext()
	}

	// Add selected projects context
	workingDir := ""
	if len(req.Projects) > 0 {
		systemPrompt += "\n\nYou have FULL FILE ACCESS to these project directories:\n"
		for _, p := range req.Projects {
			systemPrompt += "- " + p + "\n"
		}
		systemPrompt += "Use your tools (Read, Grep, Glob) to explore files when answering questions about code.\n"
		workingDir = req.Projects[0]
	}

	// Single-turn completion via REST
	modelReq := model.Request{
		Messages: []model.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: req.Message},
		},
		MaxTokens:  4096,
		Model:      req.Model,
		WorkingDir: workingDir,
	}

	resp, err := w.router.Complete(r.Context(), modelReq)
	if err != nil {
		json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(rw).Encode(map[string]interface{}{
		"response": resp.Content,
		"model":    resp.Model,
		"usage":    resp.Usage,
	})
}

func (w *WebUIServer) handleDigest(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if w.gateway == nil || w.gateway.digestScheduler == nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"error": "digest not configured",
		})
		return
	}

	data, err := w.gateway.digestScheduler.GetDigest(r.Context())
	if err != nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// Also trigger notification send
	w.gateway.digestScheduler.RunDigest(r.Context())

	json.NewEncoder(rw).Encode(map[string]interface{}{
		"status":   "ok",
		"projects": len(data.Projects),
		"data":     data,
	})
}

func (w *WebUIServer) handleIndexStats(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	if w.gateway == nil || w.gateway.indexEngine == nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"files":   0,
			"classes": 0,
		})
		return
	}

	stats, err := w.gateway.indexEngine.GetStats()
	if err != nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	json.NewEncoder(rw).Encode(map[string]interface{}{
		"total_files":   stats.Files,
		"total_classes": stats.Classes,
		"methods":       stats.Methods,
		"endpoints":     stats.Endpoints,
		"kafka_topics":  stats.KafkaTopics,
		"spring_beans":  stats.SpringBeans,
		"entities":      stats.Entities,
	})
}

func (w *WebUIServer) handleReindex(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if w.gateway == nil || w.gateway.indexEngine == nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"error": "index engine not configured",
		})
		return
	}

	// Reindex all workspace paths
	totalJava := 0
	totalKotlin := 0
	for _, wsPath := range w.gateway.cfg.General.WorkspacePaths {
		entries, err := os.ReadDir(wsPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name()[0] == '.' {
				continue
			}
			projectPath := filepath.Join(wsPath, entry.Name())
			stats, err := w.gateway.indexEngine.IndexProject(r.Context(), projectPath)
			if err == nil {
				totalJava += stats.JavaFiles
				totalKotlin += stats.KotlinFiles
			}
		}
	}

	json.NewEncoder(rw).Encode(map[string]interface{}{
		"status":         "ok",
		"files_indexed":  totalJava + totalKotlin,
	})
}

func (w *WebUIServer) handleWatcherStatus(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")

	if w.gateway == nil || w.gateway.pipelineWatcher == nil {
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"running":       false,
			"seen_failures": 0,
		})
		return
	}

	json.NewEncoder(rw).Encode(w.gateway.pipelineWatcher.GetStatus())
}
