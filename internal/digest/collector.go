package digest

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Collector gathers data from all sources for the morning digest.
type Collector struct {
	// Integration sources will be injected
}

// DigestData contains all collected information for a digest.
type DigestData struct {
	Projects    []ProjectDigest `json:"projects"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// ProjectDigest is the digest for a single project.
type ProjectDigest struct {
	Name           string          `json:"name"`
	PipelineStatus []PipelineInfo  `json:"pipeline_status"`
	PRs            []PRInfo        `json:"prs"`
	RecentCommits  []CommitInfo    `json:"recent_commits"`
	WorkItems      []WorkItemInfo  `json:"work_items"`
}

type PipelineInfo struct {
	Branch  string `json:"branch"`
	Status  string `json:"status"` // green, red, running
	Message string `json:"message"`
}

type PRInfo struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	Status  string `json:"status"` // open, approved, changes_requested
}

type CommitInfo struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

type WorkItemInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

// NewCollector creates a digest data collector.
func NewCollector() *Collector {
	return &Collector{}
}

// Collect gathers digest data from all configured sources.
func (c *Collector) Collect(ctx context.Context) (*DigestData, error) {
	data := &DigestData{
		GeneratedAt: time.Now(),
	}

	// TODO: integrate with Azure DevOps, GitLab, Git
	// For now, return placeholder
	data.Projects = append(data.Projects, ProjectDigest{
		Name: "(configure CI/CD integration for live data)",
	})

	return data, nil
}

// Format renders the digest data as a readable string.
func Format(data *DigestData) string {
	var sb strings.Builder

	sb.WriteString("📊 GreenForge Morning Digest\n")
	sb.WriteString(strings.Repeat("━", 40) + "\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", data.GeneratedAt.Format("2006-01-02 15:04")))

	for _, project := range data.Projects {
		if project.Name == "" {
			continue
		}

		sb.WriteString(fmt.Sprintf("🟢 %s\n", project.Name))
		sb.WriteString(strings.Repeat("─", 30) + "\n")

		// Pipeline status
		if len(project.PipelineStatus) > 0 {
			sb.WriteString("📊 Pipeline:\n")
			for _, p := range project.PipelineStatus {
				icon := "✅"
				if p.Status == "red" {
					icon = "🔴"
				} else if p.Status == "running" {
					icon = "🔄"
				}
				sb.WriteString(fmt.Sprintf("   %s %s %s\n", icon, p.Branch, p.Message))
			}
		}

		// PRs
		if len(project.PRs) > 0 {
			sb.WriteString(fmt.Sprintf("🔀 PRs: %d active\n", len(project.PRs)))
			for _, pr := range project.PRs {
				sb.WriteString(fmt.Sprintf("   └ #%d \"%s\" (%s) - %s\n", pr.ID, pr.Title, pr.Author, pr.Status))
			}
		}

		// Recent commits
		if len(project.RecentCommits) > 0 {
			sb.WriteString(fmt.Sprintf("📝 Commits: %d recent\n", len(project.RecentCommits)))
			for _, c := range project.RecentCommits {
				sb.WriteString(fmt.Sprintf("   └ %s: %s\n", c.Author, c.Message))
			}
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
