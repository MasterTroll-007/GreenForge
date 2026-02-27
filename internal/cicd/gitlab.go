package cicd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitLabClient implements Client for GitLab REST API.
type GitLabClient struct {
	baseURL  string
	token    string
	client   *http.Client
	projects []string // watched project paths (e.g. "group/project")
}

// NewGitLabClient creates a GitLab CI/CD client.
func NewGitLabClient(baseURL, token string, projects []string) *GitLabClient {
	return &GitLabClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		token:    token,
		client:   &http.Client{Timeout: 30 * time.Second},
		projects: projects,
	}
}

func (c *GitLabClient) Name() string { return "gitlab" }

func (c *GitLabClient) Available() bool {
	return c.baseURL != "" && c.token != ""
}

func (c *GitLabClient) doRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
}

func (c *GitLabClient) projectURL(project string) string {
	return fmt.Sprintf("%s/api/v4/projects/%s", c.baseURL, url.PathEscape(project))
}

// Pipelines returns recent pipeline runs.
func (c *GitLabClient) Pipelines(ctx context.Context, opts PipelineQuery) ([]Pipeline, error) {
	projects := c.projects
	if opts.Project != "" {
		projects = []string{opts.Project}
	}

	var all []Pipeline
	for _, project := range projects {
		pipelines, err := c.getPipelines(ctx, project, opts)
		if err != nil {
			return nil, fmt.Errorf("gitlab pipelines for %s: %w", project, err)
		}
		all = append(all, pipelines...)
	}
	return all, nil
}

func (c *GitLabClient) getPipelines(ctx context.Context, project string, opts PipelineQuery) ([]Pipeline, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 20
	}

	reqURL := fmt.Sprintf("%s/pipelines?per_page=%d&order_by=updated_at&sort=desc",
		c.projectURL(project), limit)

	if opts.Branch != "" {
		reqURL += "&ref=" + url.QueryEscape(opts.Branch)
	}
	if !opts.Since.IsZero() {
		reqURL += "&updated_after=" + opts.Since.Format(time.RFC3339)
	}

	resp, err := c.doRequest(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab API %d: %s", resp.StatusCode, string(body))
	}

	var glPipelines []struct {
		ID        int    `json:"id"`
		Ref       string `json:"ref"`
		Status    string `json:"status"` // success, failed, running, pending, canceled
		SHA       string `json:"sha"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		WebURL    string `json:"web_url"`
		User      struct {
			Name string `json:"name"`
		} `json:"user"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&glPipelines); err != nil {
		return nil, err
	}

	var pipelines []Pipeline
	for _, gl := range glPipelines {
		started, _ := time.Parse(time.RFC3339, gl.CreatedAt)
		finished, _ := time.Parse(time.RFC3339, gl.UpdatedAt)

		// Map GitLab status to our common format
		status := gl.Status
		result := gl.Status
		switch gl.Status {
		case "success":
			status = "succeeded"
			result = "succeeded"
		case "failed":
			status = "failed"
			result = "failed"
		case "running", "pending":
			status = "running"
			result = ""
		}

		p := Pipeline{
			ID:         fmt.Sprintf("%d", gl.ID),
			Project:    project,
			Branch:     gl.Ref,
			Status:     status,
			Result:     result,
			StartedAt:  started,
			FinishedAt: finished,
			URL:        gl.WebURL,
			Commit:     gl.SHA,
			Author:     gl.User.Name,
		}

		if p.IsFailed() {
			c.enrichFailureDetails(ctx, project, gl.ID, &p)
		}

		pipelines = append(pipelines, p)
	}

	return pipelines, nil
}

func (c *GitLabClient) enrichFailureDetails(ctx context.Context, project string, pipelineID int, p *Pipeline) {
	// Get failed jobs from pipeline
	reqURL := fmt.Sprintf("%s/pipelines/%d/jobs?scope[]=failed&per_page=5",
		c.projectURL(project), pipelineID)

	resp, err := c.doRequest(ctx, "GET", reqURL, nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var jobs []struct {
		Name  string `json:"name"`
		Stage string `json:"stage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return
	}

	for _, job := range jobs {
		if p.FailedStage == "" {
			p.FailedStage = job.Stage
		}
		if p.FailedJob == "" {
			p.FailedJob = job.Name
		}
	}
}

// PullRequests returns open merge requests.
func (c *GitLabClient) PullRequests(ctx context.Context, project string) ([]PullRequest, error) {
	reqURL := fmt.Sprintf("%s/merge_requests?state=opened&per_page=20", c.projectURL(project))

	resp, err := c.doRequest(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gitlab MR API %d", resp.StatusCode)
	}

	var mrs []struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		State        string `json:"state"`
		WebURL       string `json:"web_url"`
		CreatedAt    string `json:"created_at"`
		Author       struct {
			Name string `json:"name"`
		} `json:"author"`
		Reviewers []struct {
			Name string `json:"name"`
		} `json:"reviewers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&mrs); err != nil {
		return nil, err
	}

	var prs []PullRequest
	for _, mr := range mrs {
		created, _ := time.Parse(time.RFC3339, mr.CreatedAt)

		var reviewers []string
		for _, r := range mr.Reviewers {
			reviewers = append(reviewers, r.Name)
		}

		prs = append(prs, PullRequest{
			ID:           mr.IID,
			Title:        mr.Title,
			Author:       mr.Author.Name,
			SourceBranch: mr.SourceBranch,
			TargetBranch: mr.TargetBranch,
			Status:       "open",
			URL:          mr.WebURL,
			CreatedAt:    created,
			Reviewers:    reviewers,
		})
	}

	return prs, nil
}

// CreatePR creates a new merge request in GitLab.
func (c *GitLabClient) CreatePR(ctx context.Context, req CreatePRRequest) (*PullRequest, error) {
	body := map[string]interface{}{
		"source_branch": req.SourceBranch,
		"target_branch": req.TargetBranch,
		"title":         req.Title,
		"description":   req.Description,
	}

	if len(req.Labels) > 0 {
		body["labels"] = strings.Join(req.Labels, ",")
	}

	bodyJSON, _ := json.Marshal(body)
	reqURL := fmt.Sprintf("%s/merge_requests", c.projectURL(req.Project))

	resp, err := c.doRequest(ctx, "POST", reqURL, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create MR failed %d: %s", resp.StatusCode, string(respBody))
	}

	var created struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		WebURL string `json:"web_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, err
	}

	return &PullRequest{
		ID:           created.IID,
		Title:        created.Title,
		SourceBranch: req.SourceBranch,
		TargetBranch: req.TargetBranch,
		Status:       "open",
		URL:          created.WebURL,
		CreatedAt:    time.Now(),
	}, nil
}
