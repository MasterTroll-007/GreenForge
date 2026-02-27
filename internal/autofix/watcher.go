package autofix

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/greencode/greenforge/internal/cicd"
	"github.com/greencode/greenforge/internal/config"
	"github.com/greencode/greenforge/internal/notify"
)

// Watcher monitors CI/CD pipelines and triggers auto-fix when configured.
type Watcher struct {
	cfg      *config.Config
	notifier *notify.Engine
	clients  []cicd.Client
	fixer    *Fixer
	interval time.Duration

	// Track seen pipeline failures to avoid duplicate alerts
	mu       sync.Mutex
	seen     map[string]time.Time // pipeline ID -> first seen time
	fixCount map[string]int       // repo+branch -> number of auto-fixes applied
}

// NewWatcher creates a pipeline watcher.
func NewWatcher(cfg *config.Config, notifier *notify.Engine, clients []cicd.Client) *Watcher {
	return &Watcher{
		cfg:      cfg,
		notifier: notifier,
		clients:  clients,
		fixer:    NewFixer(cfg, clients),
		interval: 60 * time.Second,
		seen:     make(map[string]time.Time),
		fixCount: make(map[string]int),
	}
}

// Start begins watching pipelines in the background.
func (w *Watcher) Start(ctx context.Context) {
	// Initial check immediately
	w.check(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	log.Println("Pipeline watcher started (interval:", w.interval, ")")

	for {
		select {
		case <-ctx.Done():
			log.Println("Pipeline watcher stopped")
			return
		case <-ticker.C:
			w.check(ctx)
		}
	}
}

func (w *Watcher) check(ctx context.Context) {
	since := time.Now().Add(-2 * w.interval) // look back 2 intervals

	for _, client := range w.clients {
		if !client.Available() {
			continue
		}

		pipelines, err := client.Pipelines(ctx, cicd.PipelineQuery{
			Since: since,
			Limit: 50,
		})
		if err != nil {
			log.Printf("Pipeline watcher: %s error: %v", client.Name(), err)
			continue
		}

		for _, p := range pipelines {
			if !p.IsFailed() {
				continue
			}
			w.handleFailure(ctx, client, p)
		}
	}

	// Cleanup old seen entries (older than 24h)
	w.mu.Lock()
	cutoff := time.Now().Add(-24 * time.Hour)
	for id, t := range w.seen {
		if t.Before(cutoff) {
			delete(w.seen, id)
		}
	}
	w.mu.Unlock()
}

func (w *Watcher) handleFailure(ctx context.Context, client cicd.Client, p cicd.Pipeline) {
	// Check if already seen
	pipelineKey := fmt.Sprintf("%s:%s:%s", client.Name(), p.Project, p.ID)
	w.mu.Lock()
	if _, exists := w.seen[pipelineKey]; exists {
		w.mu.Unlock()
		return
	}
	w.seen[pipelineKey] = time.Now()
	w.mu.Unlock()

	log.Printf("Pipeline failure detected: %s/%s branch=%s", p.Project, p.ID, p.Branch)

	// Resolve auto-fix policy
	policy := ResolvePolicyForBranch(&w.cfg.AutoFix, p.Project, p.Branch)

	// Build notification message
	msg := w.buildFailureNotification(p, policy)

	switch policy {
	case "notify_only":
		w.notifier.Send(ctx, msg)

	case "fix_and_pr":
		w.notifier.Send(ctx, msg)
		w.attemptFix(ctx, client, p, false)

	case "fix_and_merge":
		w.notifier.Send(ctx, msg)
		w.attemptFix(ctx, client, p, true)

	default:
		// Unknown policy, just notify
		w.notifier.Send(ctx, msg)
	}
}

func (w *Watcher) buildFailureNotification(p cicd.Pipeline, policy string) notify.Message {
	body := fmt.Sprintf("Branch: %s\nCommit: %s\nAuthor: %s",
		p.Branch, shortHash(p.Commit), p.Author)

	if p.FailedStage != "" {
		body += fmt.Sprintf("\nFailed stage: %s", p.FailedStage)
	}
	if p.FailedJob != "" {
		body += fmt.Sprintf("\nFailed job: %s", p.FailedJob)
	}
	if p.ErrorLog != "" {
		// Truncate error log for notification
		errorLog := p.ErrorLog
		if len(errorLog) > 500 {
			errorLog = errorLog[:500] + "..."
		}
		body += fmt.Sprintf("\n\nError:\n%s", errorLog)
	}

	body += fmt.Sprintf("\nPolicy: %s", policy)

	actions := []notify.Action{
		{Label: "View Pipeline", Command: p.URL},
	}
	if policy != "notify_only" {
		actions = append(actions, notify.Action{
			Label:   "Fix It",
			Command: fmt.Sprintf("greenforge autofix --project %s --pipeline %s", p.Project, p.ID),
		})
	}

	return notify.Message{
		Title:    fmt.Sprintf("Pipeline FAILED: %s", p.Project),
		Body:     body,
		Severity: "error",
		Project:  p.Project,
		Event:    "pipeline_failure",
		Actions:  actions,
	}
}

func (w *Watcher) attemptFix(ctx context.Context, client cicd.Client, p cicd.Pipeline, autoMerge bool) {
	branchKey := fmt.Sprintf("%s:%s", p.Project, p.Branch)

	// Check fix count limit
	w.mu.Lock()
	count := w.fixCount[branchKey]
	maxFixes := w.cfg.AutoFix.MaxAutoFixes
	if maxFixes == 0 {
		maxFixes = 3
	}
	if count >= maxFixes {
		w.mu.Unlock()
		log.Printf("Auto-fix limit reached for %s (%d/%d)", branchKey, count, maxFixes)
		w.notifier.Send(ctx, notify.Message{
			Title:    fmt.Sprintf("Auto-fix limit reached: %s", p.Project),
			Body:     fmt.Sprintf("Branch %s has reached %d auto-fixes. Manual intervention required.", p.Branch, maxFixes),
			Severity: "warning",
			Project:  p.Project,
			Event:    "autofix_completed",
		})
		return
	}
	w.fixCount[branchKey] = count + 1
	w.mu.Unlock()

	// Attempt the fix
	result, err := w.fixer.Fix(ctx, FixRequest{
		Pipeline:  p,
		Client:    client,
		AutoMerge: autoMerge,
	})

	if err != nil {
		log.Printf("Auto-fix failed for %s: %v", branchKey, err)
		w.notifier.Send(ctx, notify.Message{
			Title:    fmt.Sprintf("Auto-fix FAILED: %s", p.Project),
			Body:     fmt.Sprintf("Could not auto-fix %s/%s: %s", p.Project, p.Branch, err.Error()),
			Severity: "warning",
			Project:  p.Project,
			Event:    "autofix_completed",
		})
		return
	}

	w.notifier.Send(ctx, notify.Message{
		Title:    fmt.Sprintf("Auto-fix applied: %s", p.Project),
		Body:     fmt.Sprintf("Fix applied for %s/%s\nPR: %s\nDescription: %s", p.Project, p.Branch, result.PRURL, result.Description),
		Severity: "info",
		Project:  p.Project,
		Event:    "autofix_completed",
		Actions: []notify.Action{
			{Label: "View PR", Command: result.PRURL},
			{Label: "Approve & Merge", Command: fmt.Sprintf("greenforge pr merge %d --project %s", result.PRID, p.Project)},
		},
	})
}

// GetStatus returns current watcher state for the API/UI.
func (w *Watcher) GetStatus() WatcherStatus {
	w.mu.Lock()
	defer w.mu.Unlock()

	return WatcherStatus{
		Running:      true,
		Interval:     w.interval.String(),
		SeenFailures: len(w.seen),
		FixCounts:    copyMap(w.fixCount),
	}
}

// WatcherStatus represents the current state of the pipeline watcher.
type WatcherStatus struct {
	Running      bool           `json:"running"`
	Interval     string         `json:"interval"`
	SeenFailures int            `json:"seen_failures"`
	FixCounts    map[string]int `json:"fix_counts"`
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

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

func copyMap(m map[string]int) map[string]int {
	cp := make(map[string]int, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
