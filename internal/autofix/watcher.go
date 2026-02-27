package autofix

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/greencode/greenforge/internal/config"
	"github.com/greencode/greenforge/internal/notify"
)

// Watcher monitors CI/CD pipelines and triggers auto-fix when configured.
type Watcher struct {
	cfg      *config.Config
	notifier *notify.Engine
	interval time.Duration
}

// NewWatcher creates a pipeline watcher.
func NewWatcher(cfg *config.Config, notifier *notify.Engine) *Watcher {
	return &Watcher{
		cfg:      cfg,
		notifier: notifier,
		interval: 60 * time.Second,
	}
}

// Start begins watching pipelines in the background.
func (w *Watcher) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	log.Println("Pipeline watcher started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.check(ctx)
		}
	}
}

func (w *Watcher) check(ctx context.Context) {
	// TODO: poll Azure DevOps / GitLab for pipeline status
	// For each failed pipeline, check autofix policy and act accordingly
}

// ResolvePolicyForBranch finds the applicable auto-fix policy for a repo+branch.
func ResolvePolicyForBranch(cfg *config.AutoFixConfig, repo, branch string) string {
	for _, rp := range cfg.RepoPolicies {
		if !matchPattern(rp.Repo, repo) {
			continue
		}
		for _, rule := range rp.Rules {
			if matchPattern(rule.Branch, branch) {
				return rule.OnFailure
			}
		}
	}
	return cfg.DefaultPolicy
}

// matchPattern does simple glob matching: "*" matches all, "feature/*" matches "feature/xyz".
func matchPattern(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	matched, err := filepath.Match(pattern, value)
	return err == nil && matched
}
