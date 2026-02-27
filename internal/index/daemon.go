package index

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Daemon watches project directories for changes and triggers incremental reindexing.
type Daemon struct {
	engine   *Engine
	projects []string // project root paths to watch
	interval time.Duration
	mu       sync.Mutex
	running  bool
}

// NewDaemon creates a background index daemon.
func NewDaemon(engine *Engine, projects []string) *Daemon {
	return &Daemon{
		engine:   engine,
		projects: projects,
		interval: 30 * time.Second,
	}
}

// Start begins watching for changes in the background.
func (d *Daemon) Start(ctx context.Context) {
	d.mu.Lock()
	d.running = true
	d.mu.Unlock()

	log.Printf("Index daemon started: watching %d projects (interval: %s)", len(d.projects), d.interval)

	// Do initial index of any unindexed projects
	for _, project := range d.projects {
		stats, err := d.engine.GetStats()
		if err != nil || stats.Files == 0 {
			log.Printf("Index daemon: initial indexing %s", project)
			if s, err := d.engine.IndexProject(ctx, project); err == nil {
				log.Printf("Index daemon: indexed %s (%d java + %d kotlin files)", project, s.JavaFiles, s.KotlinFiles)
			}
		}
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.mu.Lock()
			d.running = false
			d.mu.Unlock()
			log.Println("Index daemon stopped")
			return
		case <-ticker.C:
			d.checkForChanges(ctx)
		}
	}
}

// AddProject adds a project to the watch list.
func (d *Daemon) AddProject(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range d.projects {
		if p == path {
			return
		}
	}
	d.projects = append(d.projects, path)
	log.Printf("Index daemon: added project %s", path)
}

// IsRunning returns whether the daemon is active.
func (d *Daemon) IsRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

func (d *Daemon) checkForChanges(ctx context.Context) {
	for _, project := range d.projects {
		if !isGitRepo(project) {
			continue
		}

		// Check if there are changes since last index
		if d.hasGitChanges(project) {
			log.Printf("Index daemon: changes detected in %s, reindexing...", filepath.Base(project))
			stats, err := d.engine.IncrementalUpdate(ctx, project)
			if err != nil {
				log.Printf("Index daemon: incremental update failed for %s: %v", project, err)
				continue
			}
			if stats.JavaFiles+stats.KotlinFiles > 0 {
				log.Printf("Index daemon: updated %s (%d java + %d kotlin files)",
					filepath.Base(project), stats.JavaFiles, stats.KotlinFiles)
			}
		}
	}
}

// hasGitChanges checks if git reports any changes since last commit that was indexed.
func (d *Daemon) hasGitChanges(projectPath string) bool {
	// Method 1: Check if HEAD has changed since our last known commit
	headHash := getGitHead(projectPath)
	if headHash == "" {
		return false
	}

	// Check uncommitted changes (modified/added files matching our extensions)
	cmd := exec.Command("git", "-C", projectPath, "status", "--porcelain", "--short")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	if len(output) > 0 {
		// Check if any changed files are ones we care about
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			if len(line) < 3 {
				continue
			}
			file := strings.TrimSpace(line[2:])
			if isIndexableFile(file) {
				return true
			}
		}
	}

	// Method 2: Check for new commits since last check
	// Use a marker file to track last indexed commit
	markerFile := filepath.Join(projectPath, ".git", "greenforge-last-indexed")
	lastHash := ""
	if data, err := os.ReadFile(markerFile); err == nil {
		lastHash = strings.TrimSpace(string(data))
	}

	if lastHash == "" || lastHash != headHash {
		// New commit detected, update marker
		os.WriteFile(markerFile, []byte(headHash), 0644)
		return lastHash != "" // Only trigger if we had a previous hash (not first run)
	}

	return false
}

func getGitHead(projectPath string) string {
	cmd := exec.Command("git", "-C", projectPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func isGitRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func isIndexableFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".java", ".kt", ".kts", ".gradle", ".xml", ".yml", ".yaml", ".properties":
		return true
	}
	// Also check for build files
	base := filepath.Base(path)
	switch base {
	case "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts":
		return true
	}
	return false
}

// DaemonStatus returns the current status of the daemon.
type DaemonStatus struct {
	Running    bool     `json:"running"`
	Projects   []string `json:"projects"`
	Interval   string   `json:"interval"`
}

func (d *Daemon) GetStatus() DaemonStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return DaemonStatus{
		Running:  d.running,
		Projects: d.projects,
		Interval: d.interval.String(),
	}
}
