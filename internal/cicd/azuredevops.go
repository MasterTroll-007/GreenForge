package cicd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AzureDevOpsClient implements Client for Azure DevOps REST API.
type AzureDevOpsClient struct {
	organization string
	pat          string
	client       *http.Client
	baseURL      string
	projects     []string // watched project names
}

// NewAzureDevOpsClient creates an Azure DevOps CI/CD client.
func NewAzureDevOpsClient(organization, pat string, projects []string) *AzureDevOpsClient {
	return &AzureDevOpsClient{
		organization: organization,
		pat:          pat,
		client:       &http.Client{Timeout: 30 * time.Second},
		baseURL:      fmt.Sprintf("https://dev.azure.com/%s", organization),
		projects:     projects,
	}
}

func (c *AzureDevOpsClient) Name() string { return "azure_devops" }

func (c *AzureDevOpsClient) Available() bool {
	return c.organization != "" && c.pat != ""
}

func (c *AzureDevOpsClient) authHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+c.pat))
}

func (c *AzureDevOpsClient) doRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
}

// Pipelines returns recent pipeline (build) runs.
func (c *AzureDevOpsClient) Pipelines(ctx context.Context, opts PipelineQuery) ([]Pipeline, error) {
	projects := c.projects
	if opts.Project != "" {
		projects = []string{opts.Project}
	}

	var all []Pipeline
	for _, project := range projects {
		pipelines, err := c.getBuilds(ctx, project, opts)
		if err != nil {
			return nil, fmt.Errorf("azdo builds for %s: %w", project, err)
		}
		all = append(all, pipelines...)
	}
	return all, nil
}

func (c *AzureDevOpsClient) getBuilds(ctx context.Context, project string, opts PipelineQuery) ([]Pipeline, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 20
	}

	url := fmt.Sprintf("%s/%s/_apis/build/builds?api-version=7.1&$top=%d&queryOrder=startTimeDescending",
		c.baseURL, project, limit)

	if opts.Branch != "" {
		url += "&branchName=refs/heads/" + opts.Branch
	}
	if !opts.Since.IsZero() {
		url += "&minTime=" + opts.Since.Format(time.RFC3339)
	}

	resp, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("azdo API %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Value []struct {
			ID         int    `json:"id"`
			BuildNumber string `json:"buildNumber"`
			Status     string `json:"status"`     // completed, inProgress, cancelling, etc.
			Result     string `json:"result"`     // succeeded, failed, partiallySucceeded, canceled
			SourceBranch string `json:"sourceBranch"` // refs/heads/main
			StartTime  string `json:"startTime"`
			FinishTime string `json:"finishTime"`
			URL        string `json:"url"`
			RequestedFor struct {
				DisplayName string `json:"displayName"`
			} `json:"requestedFor"`
			SourceVersion string `json:"sourceVersion"`
			Links struct {
				Web struct {
					Href string `json:"href"`
				} `json:"web"`
			} `json:"_links"`
		} `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing azdo response: %w", err)
	}

	var pipelines []Pipeline
	for _, b := range result.Value {
		branch := strings.TrimPrefix(b.SourceBranch, "refs/heads/")
		started, _ := time.Parse(time.RFC3339, b.StartTime)
		finished, _ := time.Parse(time.RFC3339, b.FinishTime)

		status := b.Status
		if b.Status == "completed" {
			status = b.Result
		}

		p := Pipeline{
			ID:         fmt.Sprintf("%d", b.ID),
			Project:    project,
			Branch:     branch,
			Status:     status,
			Result:     b.Result,
			StartedAt:  started,
			FinishedAt: finished,
			URL:        b.Links.Web.Href,
			Commit:     b.SourceVersion,
			Author:     b.RequestedFor.DisplayName,
		}

		// If failed, try to get timeline for failure details
		if p.IsFailed() {
			c.enrichFailureDetails(ctx, project, b.ID, &p)
		}

		pipelines = append(pipelines, p)
	}

	return pipelines, nil
}

func (c *AzureDevOpsClient) enrichFailureDetails(ctx context.Context, project string, buildID int, p *Pipeline) {
	url := fmt.Sprintf("%s/%s/_apis/build/builds/%d/timeline?api-version=7.1", c.baseURL, project, buildID)
	resp, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var timeline struct {
		Records []struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			State  string `json:"state"`
			Result string `json:"result"`
			Issues []struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"issues"`
		} `json:"records"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&timeline); err != nil {
		return
	}

	for _, rec := range timeline.Records {
		if rec.Result == "failed" {
			if rec.Type == "Stage" {
				p.FailedStage = rec.Name
			} else if rec.Type == "Task" || rec.Type == "Job" {
				p.FailedJob = rec.Name
			}
			for _, issue := range rec.Issues {
				if issue.Type == "error" {
					if p.ErrorLog != "" {
						p.ErrorLog += "\n"
					}
					p.ErrorLog += issue.Message
				}
			}
		}
	}
}

// PullRequests returns open PRs for a project.
func (c *AzureDevOpsClient) PullRequests(ctx context.Context, project string) ([]PullRequest, error) {
	url := fmt.Sprintf("%s/%s/_apis/git/pullrequests?api-version=7.1&searchCriteria.status=active", c.baseURL, project)

	resp, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("azdo PR API %d", resp.StatusCode)
	}

	var result struct {
		Value []struct {
			PullRequestID int    `json:"pullRequestId"`
			Title         string `json:"title"`
			CreatedBy     struct {
				DisplayName string `json:"displayName"`
			} `json:"createdBy"`
			SourceRefName string `json:"sourceRefName"`
			TargetRefName string `json:"targetRefName"`
			Status        string `json:"status"`
			CreationDate  string `json:"creationDate"`
			URL           string `json:"url"`
			Reviewers     []struct {
				DisplayName string `json:"displayName"`
				Vote        int    `json:"vote"`
			} `json:"reviewers"`
		} `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var prs []PullRequest
	for _, pr := range result.Value {
		created, _ := time.Parse(time.RFC3339, pr.CreationDate)

		status := "open"
		for _, r := range pr.Reviewers {
			if r.Vote == 10 {
				status = "approved"
				break
			} else if r.Vote == -5 {
				status = "changes_requested"
				break
			}
		}

		var reviewers []string
		for _, r := range pr.Reviewers {
			reviewers = append(reviewers, r.DisplayName)
		}

		prs = append(prs, PullRequest{
			ID:           pr.PullRequestID,
			Title:        pr.Title,
			Author:       pr.CreatedBy.DisplayName,
			SourceBranch: strings.TrimPrefix(pr.SourceRefName, "refs/heads/"),
			TargetBranch: strings.TrimPrefix(pr.TargetRefName, "refs/heads/"),
			Status:       status,
			CreatedAt:    created,
			Reviewers:    reviewers,
		})
	}

	return prs, nil
}

// CreatePR creates a new pull request in Azure DevOps.
func (c *AzureDevOpsClient) CreatePR(ctx context.Context, req CreatePRRequest) (*PullRequest, error) {
	// Find the first git repo in the project
	reposURL := fmt.Sprintf("%s/%s/_apis/git/repositories?api-version=7.1", c.baseURL, req.Project)
	reposResp, err := c.doRequest(ctx, "GET", reposURL, nil)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}
	defer reposResp.Body.Close()

	var repos struct {
		Value []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := json.NewDecoder(reposResp.Body).Decode(&repos); err != nil {
		return nil, err
	}
	if len(repos.Value) == 0 {
		return nil, fmt.Errorf("no repositories found in project %s", req.Project)
	}
	repoID := repos.Value[0].ID

	// Create the PR
	prBody := map[string]interface{}{
		"sourceRefName": "refs/heads/" + req.SourceBranch,
		"targetRefName": "refs/heads/" + req.TargetBranch,
		"title":         req.Title,
		"description":   req.Description,
	}

	bodyJSON, _ := json.Marshal(prBody)
	url := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/pullrequests?api-version=7.1",
		c.baseURL, req.Project, repoID)

	resp, err := c.doRequest(ctx, "POST", url, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create PR failed %d: %s", resp.StatusCode, string(respBody))
	}

	var created struct {
		PullRequestID int    `json:"pullRequestId"`
		Title         string `json:"title"`
		URL           string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, err
	}

	return &PullRequest{
		ID:           created.PullRequestID,
		Title:        created.Title,
		SourceBranch: req.SourceBranch,
		TargetBranch: req.TargetBranch,
		Status:       "open",
		CreatedAt:    time.Now(),
	}, nil
}
