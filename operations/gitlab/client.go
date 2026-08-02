// Package gitlab provides access to GitLab via the official client-go SDK.
package gitlab

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/pratyay360/forge/v1/operations"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// normalizeBaseURL ensures a self-hosted GitLab URL points at the API v4
// endpoint, since client-go builds requests as BaseURL + "/projects" etc.
func normalizeBaseURL(raw string) string {
	u := strings.TrimSuffix(raw, "/")
	if !strings.HasSuffix(u, "/api/v4") {
		u += "/api/v4"
	}
	return u
}

// Client talks to the GitLab API on behalf of the authenticated user.
type Client struct {
	inner    *gitlab.Client
	instance string
}

// New builds a GitLab client from the given credentials. When cfg.URL is set
// (self-hosted instance), it is used as the API base URL; /api/v4 is appended
// automatically when missing.
func New(cfg operations.GitLabConfig) (*Client, error) {
	opts := []gitlab.ClientOptionFunc{}
	if cfg.URL != "" {
		opts = append(opts, gitlab.WithBaseURL(normalizeBaseURL(cfg.URL)))
	}
	c, err := gitlab.NewClient(cfg.Token, opts...)
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
	return "gitlab"
}

// ListRepos returns the projects owned by the authenticated user.
func (c *Client) ListRepos(ctx context.Context) ([]operations.Repo, error) {
	owned, simple := true, true
	projects, _, err := c.inner.Projects.ListProjects(&gitlab.ListProjectsOptions{
		Owned:       &owned,
		Simple:      &simple,
		ListOptions: gitlab.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	out := make([]operations.Repo, 0, len(projects))
	for _, p := range projects {
		out = append(out, operations.Repo{
			Forge:    "gitlab",
			Instance: c.Name(),
			FullName: p.PathWithNamespace,
			URL:      p.WebURL,
			Private:  string(p.Visibility) == "private",
		})
	}
	return out, nil
}

// CreateRepo creates a project. GitLab projects live in the authenticated
// user's namespace unless in.Owner names a group, in which case the group's
// projects API is used.
func (c *Client) CreateRepo(ctx context.Context, in operations.RepoInput) (operations.Repo, error) {
	priv := gitlab.PrivateVisibility
	if !in.Private {
		priv = gitlab.PublicVisibility
	}
	name := in.Name
	path := in.Name
	desc := in.Description
	if in.Owner != "" {
		// Namespaced create: POST /groups/{id}/projects is group-scoped; the
		// plain create API only supports user namespaces, so create with a
		// path prefixed by the group is not possible without the group id.
		// Fall back to listing is not needed: create then move is out of
		// scope, so surface the limitation clearly.
		return operations.Repo{}, fmt.Errorf("gitlab: creating a project in namespace %q requires the group id; create it in your own namespace or via the web UI", in.Owner)
	}
	project, _, err := c.inner.Projects.CreateProject(&gitlab.CreateProjectOptions{
		Name:        &name,
		Path:        &path,
		Description: &desc,
		Visibility:  &priv,
	})
	if err != nil {
		return operations.Repo{}, fmt.Errorf("creating project: %w", err)
	}
	return operations.Repo{
		Forge:    "gitlab",
		Instance: c.Name(),
		FullName: project.PathWithNamespace,
		URL:      project.WebURL,
		Private:  string(project.Visibility) == "private",
	}, nil
}

// RenameRepo renames a project. fullName is namespace/path.
func (c *Client) RenameRepo(ctx context.Context, fullName, newName string) (operations.Repo, error) {
	project, _, err := c.inner.Projects.EditProject(fullName, &gitlab.EditProjectOptions{
		Name: &newName,
		Path: &newName,
	})
	if err != nil {
		return operations.Repo{}, fmt.Errorf("renaming project: %w", err)
	}
	return operations.Repo{
		Forge:    "gitlab",
		Instance: c.Name(),
		FullName: project.PathWithNamespace,
		URL:      project.WebURL,
		Private:  string(project.Visibility) == "private",
	}, nil
}

// DeleteRepo deletes a project. fullName is namespace/path.
func (c *Client) DeleteRepo(ctx context.Context, fullName string) error {
	if _, err := c.inner.Projects.DeleteProject(fullName, &gitlab.DeleteProjectOptions{}); err != nil {
		return fmt.Errorf("deleting project: %w", err)
	}
	return nil
}

// SetVisibility changes a project's visibility. fullName is namespace/path.
func (c *Client) SetVisibility(ctx context.Context, fullName string, private bool) (operations.Repo, error) {
	vis := gitlab.PublicVisibility
	if private {
		vis = gitlab.PrivateVisibility
	}
	project, _, err := c.inner.Projects.EditProject(fullName, &gitlab.EditProjectOptions{Visibility: &vis})
	if err != nil {
		return operations.Repo{}, fmt.Errorf("changing visibility: %w", err)
	}
	return operations.Repo{
		Forge:    "gitlab",
		Instance: c.Name(),
		FullName: project.PathWithNamespace,
		URL:      project.WebURL,
		Private:  string(project.Visibility) == "private",
	}, nil
}

// ListRuns lists pipelines for a project. fullName is namespace/path.
func (c *Client) ListRuns(ctx context.Context, fullName string) ([]operations.Run, error) {
	pipelines, _, err := c.inner.Pipelines.ListProjectPipelines(fullName, &gitlab.ListProjectPipelinesOptions{
		ListOptions: gitlab.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing pipelines: %w", err)
	}
	out := make([]operations.Run, 0, len(pipelines))
	for _, p := range pipelines {
		out = append(out, operations.Run{
			Forge:    "gitlab",
			Instance: c.Name(),
			Repo:     fullName,
			ID:       p.ID,
			Name:     p.Name,
			Status:   p.Status,
			Branch:   p.Ref,
			URL:      p.WebURL,
		})
	}
	return out, nil
}

// ListWorkflows returns ErrNotSupported: GitLab has no workflow-definition
// concept; its pipelines are surfaced by ListRuns.
func (c *Client) ListWorkflows(ctx context.Context, fullName string) ([]operations.Workflow, error) {
	return nil, fmt.Errorf("%w: gitlab has no workflow definitions; see runs (pipelines)", operations.ErrNotSupported)
}

// ListProjects lists projects, which on GitLab are the repositories.
func (c *Client) ListProjects(ctx context.Context) ([]operations.Project, error) {
	owned, simple := true, true
	projects, _, err := c.inner.Projects.ListProjects(&gitlab.ListProjectsOptions{
		Owned:       &owned,
		Simple:      &simple,
		ListOptions: gitlab.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	out := make([]operations.Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, operations.Project{
			Forge:    "gitlab",
			Instance: c.Name(),
			FullName: p.PathWithNamespace,
			URL:      p.WebURL,
			Private:  string(p.Visibility) == "private",
		})
	}
	return out, nil
}

// ListIssues returns open issues assigned to the authenticated user.
func (c *Client) ListIssues(ctx context.Context) ([]operations.Issue, error) {
	state, scope := "opened", "assigned_to_me"
	issues, _, err := c.inner.Issues.ListIssues(&gitlab.ListIssuesOptions{
		State:       &state,
		Scope:       &scope,
		ListOptions: gitlab.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	out := make([]operations.Issue, 0, len(issues))
	for _, is := range issues {
		out = append(out, operations.Issue{
			Forge:    "gitlab",
			Instance: c.Name(),
			Repo:     repoFromWebURL(is.WebURL, int(is.ProjectID)),
			Number:   int(is.IID),
			Title:    is.Title,
			State:    is.State,
			URL:      is.WebURL,
		})
	}
	return out, nil
}

// ListPRs returns open merge requests across all projects.
func (c *Client) ListPRs(ctx context.Context) ([]operations.PR, error) {
	state := "opened"
	mrs, _, err := c.inner.MergeRequests.ListMergeRequests(&gitlab.ListMergeRequestsOptions{
		State:       &state,
		ListOptions: gitlab.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing merge requests: %w", err)
	}
	out := make([]operations.PR, 0, len(mrs))
	for _, mr := range mrs {
		out = append(out, operations.PR{
			Forge:    "gitlab",
			Instance: c.Name(),
			Repo:     repoFromWebURL(mr.WebURL, int(mr.ProjectID)),
			Number:   int(mr.IID),
			Title:    mr.Title,
			State:    mr.State,
			URL:      mr.WebURL,
		})
	}
	return out, nil
}

// repoFromWebURL extracts the project path from a GitLab web URL such as
// https://gitlab.com/group/project/-/issues/42. Issue and merge-request URLs
// always contain the /-/ segment; everything before it is the project path.
// Only that marker is special-cased, so projects literally named "issues" or
// "merge_requests" are parsed correctly. Falls back to the numeric project
// id when the URL cannot be parsed.
func repoFromWebURL(webURL string, projectID int) string {
	if webURL != "" {
		if u, err := url.Parse(webURL); err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			for i, p := range parts {
				if p == "-" {
					if i == 0 {
						return "" // project-less URL, e.g. a global issue
					}
					return strings.Join(parts[:i], "/")
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "/")
			}
		}
	}
	return fmt.Sprintf("project/%d", projectID)
}
