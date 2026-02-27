package digest

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/greencode/greenforge/internal/cicd"
	"github.com/greencode/greenforge/internal/config"
)

// Collector gathers data from all sources for the morning digest.
type Collector struct {
	cfg     *config.Config
	clients []cicd.Client
}

// DigestData contains all collected information for a digest.
type DigestData struct {
	Projects    []ProjectDigest `json:"projects"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// ProjectDigest is the digest for a single project.
type ProjectDigest struct {
	Name           string          `json:"name"`
	Path           string          `json:"path"`
	PipelineStatus []PipelineInfo  `json:"pipeline_status"`
	PRs            []PRInfo        `json:"prs"`
	RecentCommits  []CommitInfo    `json:"recent_commits"`
	WorkItems      []WorkItemInfo  `json:"work_items"`
}

type PipelineInfo struct {
	Branch  string `json:"branch"`
	Status  string `json:"status"` // green, red, running
	Message string `json:"message"`
	URL     string `json:"url"`
}

type PRInfo struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	Status  string `json:"status"` // open, approved, changes_requested
	URL     string `json:"url"`
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
func NewCollector(cfg *config.Config, clients []cicd.Client) *Collector {
	return &Collector{
		cfg:     cfg,
		clients: clients,
	}
}

// Collect gathers digest data from all configured sources.
func (c *Collector) Collect(ctx context.Context) (*DigestData, error) {
	data := &DigestData{
		GeneratedAt: time.Now(),
	}

	since := time.Now().Add(-24 * time.Hour)

	for _, proj := range c.cfg.Projects {
		pd := ProjectDigest{
			Name: proj.Name,
			Path: proj.Path,
		}

		// Collect pipeline status from CI/CD
		for _, client := range c.clients {
			if !client.Available() {
				continue
			}

			pipelines, err := client.Pipelines(ctx, cicd.PipelineQuery{
				Project: proj.Name,
				Since:   since,
				Limit:   10,
			})
			if err == nil {
				for _, p := range pipelines {
					status := "green"
					if p.IsFailed() {
						status = "red"
					} else if p.IsRunning() {
						status = "running"
					}

					msg := p.Branch
					if p.FailedJob != "" {
						msg += " - " + p.FailedJob
					}

					pd.PipelineStatus = append(pd.PipelineStatus, PipelineInfo{
						Branch:  p.Branch,
						Status:  status,
						Message: msg,
						URL:     p.URL,
					})
				}
			}

			// Collect PRs
			prs, err := client.PullRequests(ctx, proj.Name)
			if err == nil {
				for _, pr := range prs {
					pd.PRs = append(pd.PRs, PRInfo{
						ID:     pr.ID,
						Title:  pr.Title,
						Author: pr.Author,
						Status: pr.Status,
						URL:    pr.URL,
					})
				}
			}
		}

		// Collect git commits from local repo
		commits := c.getRecentCommits(proj.Path, since)
		pd.RecentCommits = commits

		data.Projects = append(data.Projects, pd)
	}

	// If no projects configured, add a hint
	if len(data.Projects) == 0 {
		data.Projects = append(data.Projects, ProjectDigest{
			Name: "(no projects configured - run 'greenforge config' to add projects)",
		})
	}

	return data, nil
}

// getRecentCommits reads git log from a local project path.
func (c *Collector) getRecentCommits(projectPath string, since time.Time) []CommitInfo {
	if projectPath == "" {
		return nil
	}

	sinceStr := since.Format("2006-01-02")
	cmd := exec.Command("git", "-C", projectPath, "log",
		"--since="+sinceStr,
		"--format=%H|%an|%s|%aI",
		"--max-count=20",
	)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var commits []CommitInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		t, _ := time.Parse(time.RFC3339, parts[3])
		commits = append(commits, CommitInfo{
			Hash:    parts[0][:8],
			Author:  parts[1],
			Message: parts[2],
			Time:    t,
		})
	}
	return commits
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

		// Work items
		if len(project.WorkItems) > 0 {
			sb.WriteString(fmt.Sprintf("📋 Work Items: %d\n", len(project.WorkItems)))
			for _, wi := range project.WorkItems {
				sb.WriteString(fmt.Sprintf("   └ %s \"%s\" - %s\n", wi.ID, wi.Title, wi.State))
			}
		}

		// Recent commits
		if len(project.RecentCommits) > 0 {
			// Group by author
			authors := make(map[string]int)
			for _, c := range project.RecentCommits {
				authors[c.Author]++
			}
			sb.WriteString(fmt.Sprintf("📝 Commits: %d recent by %d authors\n", len(project.RecentCommits), len(authors)))
			for author, count := range authors {
				sb.WriteString(fmt.Sprintf("   └ %s: %d commits\n", author, count))
			}
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatHTML renders the digest data as HTML for email notifications.
func FormatHTML(data *DigestData) string {
	var sb strings.Builder

	sb.WriteString(`<div style="font-family:sans-serif;max-width:600px;margin:0 auto">`)
	sb.WriteString(`<h2 style="color:#2ea44f">📊 GreenForge Morning Digest</h2>`)
	sb.WriteString(fmt.Sprintf(`<p style="color:#888">%s</p>`, data.GeneratedAt.Format("2006-01-02 15:04")))

	for _, project := range data.Projects {
		if project.Name == "" {
			continue
		}

		sb.WriteString(fmt.Sprintf(`<h3 style="border-bottom:1px solid #333;padding-bottom:4px">%s</h3>`, project.Name))

		// Pipeline status
		if len(project.PipelineStatus) > 0 {
			sb.WriteString(`<p><strong>Pipeline:</strong></p><ul>`)
			for _, p := range project.PipelineStatus {
				color := "#2ea44f"
				icon := "✅"
				if p.Status == "red" {
					color = "#d73a49"
					icon = "🔴"
				} else if p.Status == "running" {
					color = "#dbab09"
					icon = "🔄"
				}
				sb.WriteString(fmt.Sprintf(`<li style="color:%s">%s %s %s</li>`, color, icon, p.Branch, p.Message))
			}
			sb.WriteString(`</ul>`)
		}

		// PRs
		if len(project.PRs) > 0 {
			sb.WriteString(fmt.Sprintf(`<p><strong>🔀 PRs: %d active</strong></p><ul>`, len(project.PRs)))
			for _, pr := range project.PRs {
				link := pr.Title
				if pr.URL != "" {
					link = fmt.Sprintf(`<a href="%s">%s</a>`, pr.URL, pr.Title)
				}
				sb.WriteString(fmt.Sprintf(`<li>#%d %s (%s) - %s</li>`, pr.ID, link, pr.Author, pr.Status))
			}
			sb.WriteString(`</ul>`)
		}

		// Commits
		if len(project.RecentCommits) > 0 {
			authors := make(map[string]int)
			for _, c := range project.RecentCommits {
				authors[c.Author]++
			}
			sb.WriteString(fmt.Sprintf(`<p><strong>📝 %d commits by %d authors</strong></p>`, len(project.RecentCommits), len(authors)))
		}
	}

	sb.WriteString(`<hr><p style="color:#888;font-size:12px">GreenForge AI Developer Agent</p></div>`)
	return sb.String()
}
