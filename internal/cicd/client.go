package cicd

import (
	"context"
	"time"
)

// Client is the common interface for CI/CD platform integrations.
type Client interface {
	Name() string
	Available() bool
	// Pipelines returns recent pipeline runs for all watched projects.
	Pipelines(ctx context.Context, opts PipelineQuery) ([]Pipeline, error)
	// PullRequests returns open pull/merge requests.
	PullRequests(ctx context.Context, project string) ([]PullRequest, error)
	// CreatePR creates a new pull/merge request.
	CreatePR(ctx context.Context, req CreatePRRequest) (*PullRequest, error)
}

// PipelineQuery filters pipeline results.
type PipelineQuery struct {
	Project string    // filter by project/repo name
	Branch  string    // filter by branch
	Since   time.Time // only pipelines after this time
	Limit   int       // max results (0 = default 20)
}

// Pipeline represents a CI/CD pipeline run.
type Pipeline struct {
	ID         string    `json:"id"`
	Project    string    `json:"project"`
	Branch     string    `json:"branch"`
	Status     string    `json:"status"` // succeeded, failed, running, canceled
	Result     string    `json:"result"` // succeeded, failed, partiallySucceeded
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	URL        string    `json:"url"`
	Commit     string    `json:"commit"`
	Author     string    `json:"author"`
	// Failure details (populated when status=failed)
	FailedStage string `json:"failed_stage,omitempty"`
	FailedJob   string `json:"failed_job,omitempty"`
	ErrorLog    string `json:"error_log,omitempty"`
}

// PullRequest represents a PR/MR.
type PullRequest struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	SourceBranch string   `json:"source_branch"`
	TargetBranch string   `json:"target_branch"`
	Status      string    `json:"status"` // open, merged, closed, approved, changes_requested
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"created_at"`
	Reviewers   []string  `json:"reviewers,omitempty"`
}

// CreatePRRequest contains parameters for creating a PR.
type CreatePRRequest struct {
	Project      string   `json:"project"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	SourceBranch string   `json:"source_branch"`
	TargetBranch string   `json:"target_branch"`
	Assignees    []string `json:"assignees,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	AutoMerge    bool     `json:"auto_merge"`
}

// IsFailed returns true if the pipeline failed.
func (p Pipeline) IsFailed() bool {
	return p.Status == "failed" || p.Result == "failed"
}

// IsRunning returns true if the pipeline is still running.
func (p Pipeline) IsRunning() bool {
	return p.Status == "running" || p.Status == "inProgress"
}
