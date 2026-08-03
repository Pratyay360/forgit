// Package github provides access to GitHub via the go-github SDK.
package github

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v89/github"
	"github.com/pratyay360/forge/operations"
)

// Client talks to the GitHub API on behalf of the authenticated user.
type Client struct {
	inner    *github.Client
	instance string
}

// New builds a GitHub client from the given credentials.
func New(cfg operations.GitHubConfig) (*Client, error) {
	c, err := github.NewClient(github.WithAuthToken(cfg.Token))
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}
	return &Client{inner: c}, nil
}

// SetInstance names this client's instance (default: the forge type).
func (c *Client) SetInstance(name string) { c.instance = name }

// Name returns the instance name.
func (c *Client) Name() string {
	if c.instance != "" {
		return c.instance
	}
	return "github"
}

// maxPRRepos caps how many repositories are scanned for pull requests so the
// command stays snappy for accounts with many repositories.
const maxPRRepos = 10

// ListRepos returns the authenticated user's repositories.
func (c *Client) ListRepos(ctx context.Context) ([]operations.Repo, error) {
	repos, _, err := c.inner.Repositories.ListByAuthenticatedUser(ctx, &github.RepositoryListByAuthenticatedUserOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	out := make([]operations.Repo, 0, len(repos))
	for _, r := range repos {
		out = append(out, operations.Repo{
			Forge:    "github",
			Instance: c.Name(),
			FullName: deref(r.FullName),
			URL:      deref(r.HTMLURL),
			Private:  deref(r.Private),
		})
	}
	return out, nil
}

// CreateRepo creates a repository, owned by the authenticated user when
// input.Owner is empty, or by the given organization otherwise.
func (c *Client) CreateRepo(ctx context.Context, in operations.RepoInput) (operations.Repo, error) {
	name := in.Name
	priv := in.Private
	desc := in.Description
	repo, _, err := c.inner.Repositories.Create(ctx, in.Owner, &github.Repository{
		Name:        &name,
		Private:     &priv,
		Description: &desc,
	})
	if err != nil {
		return operations.Repo{}, fmt.Errorf("creating repository: %w", err)
	}
	return operations.Repo{
		Forge:    "github",
		Instance: c.Name(),
		FullName: deref(repo.FullName),
		URL:      deref(repo.HTMLURL),
		Private:  deref(repo.Private),
	}, nil
}

// RenameRepo renames a repository. fullName is owner/name.
func (c *Client) RenameRepo(ctx context.Context, fullName, newName string) (operations.Repo, error) {
	owner, name := splitFullName(fullName)
	repo, _, err := c.inner.Repositories.Edit(ctx, owner, name, &github.Repository{Name: &newName})
	if err != nil {
		return operations.Repo{}, fmt.Errorf("renaming repository: %w", err)
	}
	return operations.Repo{
		Forge:    "github",
		Instance: c.Name(),
		FullName: deref(repo.FullName),
		URL:      deref(repo.HTMLURL),
		Private:  deref(repo.Private),
	}, nil
}

// DeleteRepo deletes a repository. fullName is owner/name.
func (c *Client) DeleteRepo(ctx context.Context, fullName string) error {
	owner, name := splitFullName(fullName)
	if _, err := c.inner.Repositories.Delete(ctx, owner, name); err != nil {
		return fmt.Errorf("deleting repository: %w", err)
	}
	return nil
}

// SetVisibility changes whether a repository is private. fullName is owner/name.
func (c *Client) SetVisibility(ctx context.Context, fullName string, private bool) (operations.Repo, error) {
	owner, name := splitFullName(fullName)
	repo, _, err := c.inner.Repositories.Edit(ctx, owner, name, &github.Repository{Private: &private})
	if err != nil {
		return operations.Repo{}, fmt.Errorf("changing visibility: %w", err)
	}
	return operations.Repo{
		Forge:    "github",
		Instance: c.Name(),
		FullName: deref(repo.FullName),
		URL:      deref(repo.HTMLURL),
		Private:  deref(repo.Private),
	}, nil
}

// ListRuns lists workflow runs for a repository. fullName is owner/name.
func (c *Client) ListRuns(ctx context.Context, fullName string) ([]operations.Run, error) {
	owner, name := splitFullName(fullName)
	runs, _, err := c.inner.Actions.ListRepositoryWorkflowRuns(ctx, owner, name, &github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing workflow runs: %w", err)
	}
	out := make([]operations.Run, 0, len(runs.WorkflowRuns))
	for _, r := range runs.WorkflowRuns {
		status := deref(r.Status)
		if c := deref(r.Conclusion); c != "" {
			status = c
		}
		out = append(out, operations.Run{
			Forge:    "github",
			Instance: c.Name(),
			Repo:     fullName,
			ID:       deref(r.ID),
			Name:     deref(r.Name),
			Status:   status,
			Branch:   deref(r.HeadBranch),
			URL:      deref(r.HTMLURL),
		})
	}
	return out, nil
}

// ListWorkflows lists workflow definitions for a repository.
func (c *Client) ListWorkflows(ctx context.Context, fullName string) ([]operations.Workflow, error) {
	owner, name := splitFullName(fullName)
	workflows, _, err := c.inner.Actions.ListWorkflows(ctx, owner, name, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("listing workflows: %w", err)
	}
	out := make([]operations.Workflow, 0, len(workflows.Workflows))
	for _, w := range workflows.Workflows {
		out = append(out, operations.Workflow{
			Forge:    "github",
			Instance: c.Name(),
			Repo:     fullName,
			ID:       deref(w.ID),
			Name:     deref(w.Name),
			State:    deref(w.State),
			URL:      deref(w.HTMLURL),
		})
	}
	return out, nil
}

// ListProjects lists the authenticated user's (classic) projects.
func (c *Client) ListProjects(ctx context.Context) ([]operations.Project, error) {
	me, _, err := c.inner.Users.Get(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("resolving current user: %w", err)
	}
	login := deref(me.Login)
	projects, _, err := c.inner.Projects.ListUserProjects(ctx, login, &github.ListProjectsOptions{
		ListProjectsPaginationOptions: github.ListProjectsPaginationOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	out := make([]operations.Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, operations.Project{
			Forge:    "github",
			Instance: c.Name(),
			FullName: deref(p.Name),
			URL:      deref(p.HTMLURL),
			Private:  !deref(p.Public),
		})
	}
	return out, nil
}

// ListIssues returns open issues assigned to the authenticated user.
func (c *Client) ListIssues(ctx context.Context) ([]operations.Issue, error) {
	issues, _, err := c.inner.Issues.ListUserIssues(ctx, &github.ListUserIssuesOptions{
		Filter:      "assigned",
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	out := make([]operations.Issue, 0, len(issues))
	for _, is := range issues {
		if is.IsPullRequest() {
			continue // GitHub returns pull requests in the issues API.
		}
		out = append(out, operations.Issue{
			Forge:    "github",
			Instance: c.Name(),
			Repo:     repoFullName(is),
			Number:   int(deref(is.Number)),
			Title:    deref(is.Title),
			State:    deref(is.State),
			URL:      deref(is.HTMLURL),
		})
	}
	return out, nil
}

// ListPRs returns open pull requests across the user's repositories.
func (c *Client) ListPRs(ctx context.Context) ([]operations.PR, error) {
	repos, _, err := c.inner.Repositories.ListByAuthenticatedUser(ctx, &github.RepositoryListByAuthenticatedUserOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	if len(repos) > maxPRRepos {
		repos = repos[:maxPRRepos]
	}
	var out []operations.PR
	skipped := 0
	for _, r := range repos {
		if r.Owner == nil || r.Name == nil {
			continue
		}
		prs, _, err := c.inner.PullRequests.List(ctx, deref(r.Owner.Login), deref(r.Name), &github.PullRequestListOptions{
			State:       "open",
			ListOptions: github.ListOptions{PerPage: 50},
		})
		if err != nil {
			skipped++
			continue // skip repositories that fail; the overall command still works
		}
		for _, pr := range prs {
			out = append(out, operations.PR{
				Forge:    "github",
				Instance: c.Name(),
				Repo:     deref(r.FullName),
				Number:   int(deref(pr.Number)),
				Title:    deref(pr.Title),
				State:    deref(pr.State),
				URL:      deref(pr.HTMLURL),
			})
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "warning: %s: skipped %d repository(ies)\n", c.Name(), skipped)
	}
	return out, nil
}

// repoFullName derives the repository name from an issue, falling back to its
// API repository URL when the Repository object is not populated.
func repoFullName(is *github.Issue) string {
	if is.Repository != nil {
		if n := deref(is.Repository.FullName); n != "" {
			return n
		}
	}
	ru := deref(is.RepositoryURL)
	if prefix := "https://api.github.com/repos/"; strings.HasPrefix(ru, prefix) {
		return strings.TrimPrefix(ru, prefix)
	}
	return ""
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// splitFullName splits "owner/name" into its parts, returning the full string
// as the name when there is no slash (e.g. a bare repository name).
func splitFullName(full string) (owner, name string) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}
