// Package forgejo provides access to Forgejo via the forgejo-sdk.
package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	fj "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"
	"github.com/pratyay360/forgit/operations"
)

type Client struct {
	inner    *fj.Client
	baseURL  string
	token    string
	http     *http.Client
	instance string
}

// New builds a Forgejo client from the given credentials.
func New(cfg operations.ForgejoConfig) (*Client, error) {
	base := cfg.URL
	if base == "" {
		base = "https://codeberg.org"
	}
	base = strings.TrimRight(base, "/")
	c, err := fj.NewClient(base, fj.SetToken(cfg.Token))
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}
	return &Client{
		inner:   c,
		baseURL: base,
		token:   cfg.Token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// SetInstance names this client's instance (default: the forge type).
func (c *Client) SetInstance(name string) { c.instance = name }

// Name returns the instance name.
func (c *Client) Name() string {
	if c.instance != "" {
		return c.instance
	}
	return "forgejo"
}

// maxPRRepos caps how many repositories are scanned for pull requests.
const maxPRRepos = 10

// ListRepos returns the repositories the authenticated user has access to.
func (c *Client) ListRepos(ctx context.Context) ([]operations.Repo, error) {
	repos, _, err := c.inner.ListMyRepos(fj.ListReposOptions{
		ListOptions: fj.ListOptions{PageSize: 50},
	})
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	out := make([]operations.Repo, 0, len(repos))
	for _, r := range repos {
		out = append(out, operations.Repo{
			Forge:    "forgejo",
			Instance: c.Name(),
			FullName: r.FullName,
			URL:      r.HTMLURL,
			Private:  r.Private,
		})
	}
	return out, nil
}

// CreateRepo creates a repository in the authenticated user's namespace, or
// in an organization when in.Owner is set.
func (c *Client) CreateRepo(ctx context.Context, in operations.RepoInput) (operations.Repo, error) {
	opt := fj.CreateRepoOption{
		Name:        in.Name,
		Description: in.Description,
		Private:     in.Private,
	}
	var (
		repo *fj.Repository
		err  error
	)
	if in.Owner != "" {
		repo, _, err = c.inner.CreateOrgRepo(in.Owner, opt)
	} else {
		repo, _, err = c.inner.CreateRepo(opt)
	}
	if err != nil {
		return operations.Repo{}, fmt.Errorf("creating repository: %w", err)
	}
	return operations.Repo{
		Forge:    "forgejo",
		Instance: c.Name(),
		FullName: repo.FullName,
		URL:      repo.HTMLURL,
		Private:  repo.Private,
	}, nil
}

// RenameRepo renames a repository. fullName is owner/name.
func (c *Client) RenameRepo(ctx context.Context, fullName, newName string) (operations.Repo, error) {
	owner, name, ok := splitFullName(fullName)
	if !ok {
		return operations.Repo{}, fmt.Errorf("invalid repository name %q", fullName)
	}
	repo, _, err := c.inner.EditRepo(owner, name, fj.EditRepoOption{Name: &newName})
	if err != nil {
		return operations.Repo{}, fmt.Errorf("renaming repository: %w", err)
	}
	return operations.Repo{
		Forge:    "forgejo",
		Instance: c.Name(),
		FullName: repo.FullName,
		URL:      repo.HTMLURL,
		Private:  repo.Private,
	}, nil
}

// DeleteRepo deletes a repository. fullName is owner/name.
func (c *Client) DeleteRepo(ctx context.Context, fullName string) error {
	owner, name, ok := splitFullName(fullName)
	if !ok {
		return fmt.Errorf("invalid repository name %q", fullName)
	}
	if _, err := c.inner.DeleteRepo(owner, name); err != nil {
		return fmt.Errorf("deleting repository: %w", err)
	}
	return nil
}

// SetVisibility changes whether a repository is private. fullName is owner/name.
func (c *Client) SetVisibility(ctx context.Context, fullName string, private bool) (operations.Repo, error) {
	owner, name, ok := splitFullName(fullName)
	if !ok {
		return operations.Repo{}, fmt.Errorf("invalid repository name %q", fullName)
	}
	repo, _, err := c.inner.EditRepo(owner, name, fj.EditRepoOption{Private: &private})
	if err != nil {
		return operations.Repo{}, fmt.Errorf("changing visibility: %w", err)
	}
	return operations.Repo{
		Forge:    "forgejo",
		Instance: c.Name(),
		FullName: repo.FullName,
		URL:      repo.HTMLURL,
		Private:  repo.Private,
	}, nil
}

// getAPI performs a GET against the instance's /api/v1 surface and decodes
// the JSON body into out. Used for endpoints the SDK does not expose.
func (c *Client) getAPI(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1"+endpoint, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("forgejo API %s: %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ListRuns lists Actions task runs for a repository. fullName is owner/name.
//
// The SDK does not wrap the Actions API, so /repos/{owner}/{repo}/actions/tasks
// is called directly. Tasks carry the workflow name, branch and run number,
// which map onto the unified Run shape.
func (c *Client) ListRuns(ctx context.Context, fullName string) ([]operations.Run, error) {
	owner, name, ok := splitFullName(fullName)
	if !ok {
		return nil, fmt.Errorf("invalid repository name %q", fullName)
	}
	var body struct {
		Entries []struct {
			ID           int64  `json:"id"`
			Name         string `json:"name"`
			DisplayTitle string `json:"display_title"`
			Status       string `json:"status"`
			HeadBranch   string `json:"head_branch"`
			URL          string `json:"url"`
		} `json:"workflow_runs"`
	}
	endpoint := fmt.Sprintf("/repos/%s/%s/actions/tasks?limit=50",
		url.PathEscape(owner), url.PathEscape(name))
	if err := c.getAPI(ctx, endpoint, &body); err != nil {
		return nil, fmt.Errorf("listing action runs: %w", err)
	}
	out := make([]operations.Run, 0, len(body.Entries))
	for _, t := range body.Entries {
		title := t.Name
		if title == "" {
			title = t.DisplayTitle
		}
		out = append(out, operations.Run{
			Forge:    "forgejo",
			Instance: c.Name(),
			Repo:     fullName,
			ID:       t.ID,
			Name:     title,
			Status:   t.Status,
			Branch:   t.HeadBranch,
			URL:      t.URL,
		})
	}
	return out, nil
}

// workflowDirs are the directories Forgejo scans for Actions workflow
// definitions, in precedence order.
var workflowDirs = []string{".forgejo/workflows", ".github/workflows"}

// ListWorkflows lists Actions workflow definitions for a repository.
// fullName is owner/name.
//
// Forgejo has no endpoint that enumerates workflows (unlike GitHub's
// /actions/workflows), so the workflow directories are listed from the default
// branch instead. Forgejo reads .forgejo/workflows first and falls back to
// .github/workflows; both are reported here, deduplicated by file name.
func (c *Client) ListWorkflows(ctx context.Context, fullName string) ([]operations.Workflow, error) {
	owner, name, ok := splitFullName(fullName)
	if !ok {
		return nil, fmt.Errorf("invalid repository name %q", fullName)
	}
	var out []operations.Workflow
	seen := make(map[string]bool)
	found := false
	for _, dir := range workflowDirs {
		entries, _, err := c.inner.ListContents(owner, name, "", dir)
		if err != nil {
			// A missing directory is normal; only one of the two is common.
			continue
		}
		found = true
		for _, e := range entries {
			if e == nil || e.Type != "file" || !isWorkflowFile(e.Name) || seen[e.Name] {
				continue
			}
			seen[e.Name] = true
			link := ""
			if e.HTMLURL != nil {
				link = *e.HTMLURL
			}
			out = append(out, operations.Workflow{
				Forge:    "forgejo",
				Instance: c.Name(),
				Repo:     fullName,
				Name:     e.Name,
				State:    "active",
				URL:      link,
			})
		}
	}
	if !found {
		return nil, fmt.Errorf("listing workflows: no workflow directory (%s) in %s",
			strings.Join(workflowDirs, " or "), fullName)
	}
	return out, nil
}

// isWorkflowFile reports whether a file name is an Actions workflow manifest.
func isWorkflowFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".yml", ".yaml":
		return true
	}
	return false
}

// ListProjects returns ErrNotSupported: Forgejo has no projects/boards API.
// The web UI has no project boards either, so there is nothing to enumerate;
// issues and pull requests remain available.
func (c *Client) ListProjects(ctx context.Context) ([]operations.Project, error) {
	return nil, fmt.Errorf("%w: forgejo has no projects API", operations.ErrNotSupported)
}

// ListIssues returns open issues assigned to the authenticated user.
//
// The /repos/issues/search endpoint returns pull requests too unless
// type=issues is set (the SDK sends type= only when IssueType is non-empty),
// and it does not restrict results to the current user. Resolve the
// authenticated user and keep only issues assigned to them, mirroring the
// GitHub (filter=assigned) and GitLab (scope=assigned_to_me) semantics.
func (c *Client) ListIssues(ctx context.Context) ([]operations.Issue, error) {
	me, _, err := c.inner.GetMyUserInfo()
	if err != nil {
		return nil, fmt.Errorf("resolving current user: %w", err)
	}
	issues, _, err := c.inner.ListIssues(fj.ListIssueOption{
		State:       fj.StateOpen,
		Type:        fj.IssueTypeIssue,
		ListOptions: fj.ListOptions{PageSize: 50},
	})
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	out := make([]operations.Issue, 0, len(issues))
	for _, is := range issues {
		if !assignedTo(me, is) {
			continue
		}
		repo := ""
		if is.Repository != nil {
			repo = is.Repository.FullName
		}
		out = append(out, operations.Issue{
			Forge:    "forgejo",
			Instance: c.Name(),
			Repo:     repo,
			Number:   int(is.Index),
			Title:    is.Title,
			State:    string(is.State),
			URL:      is.HTMLURL,
		})
	}
	return out, nil
}

// assignedTo reports whether the issue is assigned to the given user.
func assignedTo(me *fj.User, is *fj.Issue) bool {
	if me == nil {
		return false
	}
	for _, a := range is.Assignees {
		if a != nil && a.UserName == me.UserName {
			return true
		}
	}
	return false
}

// ListPRs returns open pull requests across the user's repositories.
func (c *Client) ListPRs(ctx context.Context) ([]operations.PR, error) {
	repos, _, err := c.inner.ListMyRepos(fj.ListReposOptions{
		ListOptions: fj.ListOptions{PageSize: 50},
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
		owner, name, ok := splitFullName(r.FullName)
		if !ok {
			continue
		}
		prs, _, err := c.inner.ListRepoPullRequests(owner, name, fj.ListPullRequestsOptions{
			State:       fj.StateOpen,
			ListOptions: fj.ListOptions{PageSize: 50},
		})
		if err != nil {
			skipped++
			continue // skip repositories that fail; the overall command still works
		}
		for _, pr := range prs {
			out = append(out, operations.PR{
				Forge:    "forgejo",
				Instance: c.Name(),
				Repo:     r.FullName,
				Number:   int(pr.Index),
				Title:    pr.Title,
				State:    string(pr.State),
				URL:      pr.HTMLURL,
			})
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "warning: %s: skipped %d repository(ies)\n", c.Name(), skipped)
	}
	return out, nil
}

func splitFullName(full string) (owner, name string, ok bool) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
