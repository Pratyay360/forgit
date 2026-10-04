// Package bitbucket provides access to Bitbucket Cloud via its REST API v2.
// The go-bitbucket SDK is awkward for listing issues and pull requests
// (it returns untyped interface{}), so this client talks to the API directly
// using the app-password style basic auth.

package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pratyay360/forgit/operations"
)

// apiBase is overridable so tests can point the client at an httptest server.
var apiBase = "https://api.bitbucket.org/2.0"

// Client talks to the Bitbucket REST API on behalf of the configured user.
type Client struct {
	username   string
	token      string
	http       *http.Client
	instance   string
	forgeLabel string // user-visible forge name; defaults to "bitbucket"
}

// New builds a Bitbucket client from the given credentials.
func New(cfg operations.BitbucketConfig) (*Client, error) {
	if cfg.Username == "" {
		return nil, fmt.Errorf("bitbucket: username is required (set [bitbucket] username in config or %s)", operations.EnvBitbucketUsername)
	}
	return &Client{
		username:   cfg.Username,
		token:      cfg.Token,
		http:       &http.Client{Timeout: 30 * time.Second},
		forgeLabel: "bitbucket",
	}, nil
}

// SetInstance names this client's instance (default: the forge type).
func (c *Client) SetInstance(name string) { c.instance = name }

// Name returns the instance name.
func (c *Client) Name() string {
	if c.instance != "" {
		return c.instance
	}
	return "bitbucket"
}

// SetForgeLabel overrides the forge label the client stamps on returned
// resources. Used to give user-named aliases the right identity in listings
// while the Bitbucket client does the API work.
func (c *Client) SetForgeLabel(label string) {
	if label != "" {
		c.forgeLabel = label
	}
}

// forgeLabelOrDefault returns the label to stamp on returned resources,
// falling back to "bitbucket" when SetForgeLabel was never called.
func (c *Client) forgeLabelOrDefault() string {
	if c.forgeLabel != "" {
		return c.forgeLabel
	}
	return "bitbucket"
}

// maxPRRepos caps how many repositories are scanned for issues and pull
// requests so the commands stay snappy for workspaces with many repositories.
const maxPRRepos = 10

type repoItem struct {
	FullName  string `json:"full_name"`
	IsPrivate bool   `json:"is_private"`
	Links     struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

type prItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

// ListRepos returns the repositories in the configured workspace.
func (c *Client) ListRepos(ctx context.Context) ([]operations.Repo, error) {
	var body struct {
		Values []repoItem `json:"values"`
	}
	u := fmt.Sprintf("%s/repositories/%s?pagelen=100&role=member", apiBase, url.PathEscape(c.username))
	if err := c.get(ctx, u, &body); err != nil {
		return nil, err
	}
	out := make([]operations.Repo, 0, len(body.Values))
	for _, r := range body.Values {
		repoURL := r.Links.HTML.Href
		if repoURL == "" {
			repoURL = "https://bitbucket.org/" + r.FullName
		}
		out = append(out, operations.Repo{
			Forge:    c.forgeLabelOrDefault(),
			Instance: c.Name(),
			FullName: r.FullName,
			URL:      repoURL,
			Private:  r.IsPrivate,
		})
	}
	return out, nil
}

// CreateRepo creates a repository in the configured workspace.
func (c *Client) CreateRepo(ctx context.Context, in operations.RepoInput) (operations.Repo, error) {
	ws := in.Owner
	if ws == "" {
		ws = c.username
	}
	var body struct {
		FullName  string `json:"full_name"`
		IsPrivate bool   `json:"is_private"`
		Links     struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	u := fmt.Sprintf("%s/repositories/%s/%s", apiBase, url.PathEscape(ws), url.PathEscape(in.Name))
	if err := c.post(ctx, u, map[string]any{
		"is_private":  in.Private,
		"scm":         "git",
		"description": in.Description,
	}, &body); err != nil {
		return operations.Repo{}, fmt.Errorf("creating repository: %w", err)
	}
	return operations.Repo{
		Forge:    c.forgeLabelOrDefault(),
		Instance: c.Name(),
		FullName: body.FullName,
		URL:      body.Links.HTML.Href,
		Private:  body.IsPrivate,
	}, nil
}

// RenameRepo renames a repository. fullName is workspace/repo.
//
// Bitbucket has no dedicated rename endpoint; PUT on the repository accepts a
// new name and re-slugifies the location (the new URL comes back in the
// Location header, and in the body's full_name). The slug is derived from the
// name, so the request fails if the resulting slug collides with an existing
// repository in the workspace.
func (c *Client) RenameRepo(ctx context.Context, fullName, newName string) (operations.Repo, error) {
	var body struct {
		FullName  string `json:"full_name"`
		IsPrivate bool   `json:"is_private"`
		Links     struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	u := fmt.Sprintf("%s/repositories/%s", apiBase, fullNamePath(fullName))
	if err := c.put(ctx, u, map[string]any{"name": newName}, &body); err != nil {
		return operations.Repo{}, fmt.Errorf("renaming repository: %w", err)
	}
	return operations.Repo{
		Forge:    c.forgeLabelOrDefault(),
		Instance: c.Name(),
		FullName: body.FullName,
		URL:      body.Links.HTML.Href,
		Private:  body.IsPrivate,
	}, nil
}

// DeleteRepo deletes a repository. fullName is workspace/repo.
func (c *Client) DeleteRepo(ctx context.Context, fullName string) error {
	if err := c.delete(ctx, fmt.Sprintf("%s/repositories/%s", apiBase, fullNamePath(fullName))); err != nil {
		return fmt.Errorf("deleting repository: %w", err)
	}
	return nil
}

// SetVisibility changes whether a repository is private. fullName is
// workspace/repo.
func (c *Client) SetVisibility(ctx context.Context, fullName string, private bool) (operations.Repo, error) {
	var body struct {
		FullName  string `json:"full_name"`
		IsPrivate bool   `json:"is_private"`
		Links     struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	u := fmt.Sprintf("%s/repositories/%s", apiBase, fullNamePath(fullName))
	if err := c.put(ctx, u, map[string]any{"is_private": private}, &body); err != nil {
		return operations.Repo{}, fmt.Errorf("changing visibility: %w", err)
	}
	return operations.Repo{
		Forge:    c.forgeLabelOrDefault(),
		Instance: c.Name(),
		FullName: body.FullName,
		URL:      body.Links.HTML.Href,
		Private:  body.IsPrivate,
	}, nil
}

// ListRuns lists pipeline runs for a repository. fullName is workspace/repo.
func (c *Client) ListRuns(ctx context.Context, fullName string) ([]operations.Run, error) {
	var body struct {
		Values []struct {
			UUID  string `json:"uuid"`
			State struct {
				Name   string `json:"name"`
				Result struct {
					Name string `json:"name"`
				} `json:"result"`
			} `json:"state"`
			Target struct {
				RefName string `json:"ref_name"`
			} `json:"target"`
			Links struct {
				HTML struct {
					Href string `json:"href"`
				} `json:"html"`
			} `json:"links"`
		} `json:"values"`
	}
	u := fmt.Sprintf("%s/repositories/%s/pipelines/?pagelen=50", apiBase, fullNamePath(fullName))
	if err := c.get(ctx, u, &body); err != nil {
		return nil, fmt.Errorf("listing pipelines: %w", err)
	}
	out := make([]operations.Run, 0, len(body.Values))
	for _, p := range body.Values {
		status := p.State.Name
		if p.State.Result.Name != "" {
			status = p.State.Result.Name
		}
		out = append(out, operations.Run{
			Forge:    c.forgeLabelOrDefault(),
			Instance: c.Name(),
			Repo:     fullName,
			ID:       runIDFromUUID(p.UUID),
			Status:   status,
			Branch:   p.Target.RefName,
			URL:      p.Links.HTML.Href,
		})
	}
	return out, nil
}

// ListWorkflows returns ErrNotSupported: Bitbucket pipelines are surfaced by
// ListRuns and there is no workflow-definition API.
func (c *Client) ListWorkflows(ctx context.Context, fullName string) ([]operations.Workflow, error) {
	return nil, fmt.Errorf("%w: bitbucket has no workflow definitions; see runs (pipelines)", operations.ErrNotSupported)
}

// ListProjects lists workspace projects.
func (c *Client) ListProjects(ctx context.Context) ([]operations.Project, error) {
	var body struct {
		Values []struct {
			Name  string `json:"name"`
			Key   string `json:"key"`
			Links struct {
				HTML struct {
					Href string `json:"href"`
				} `json:"html"`
			} `json:"links"`
		} `json:"values"`
	}
	u := fmt.Sprintf("%s/workspaces/%s/projects?pagelen=100", apiBase, url.PathEscape(c.username))
	if err := c.get(ctx, u, &body); err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	out := make([]operations.Project, 0, len(body.Values))
	for _, p := range body.Values {
		out = append(out, operations.Project{
			Forge:    c.forgeLabelOrDefault(),
			Instance: c.Name(),
			FullName: p.Key,
			URL:      p.Links.HTML.Href,
		})
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, endpoint string, payload any, out interface{}) error {
	return c.request(ctx, http.MethodPost, endpoint, payload, out)
}

func (c *Client) put(ctx context.Context, endpoint string, payload any, out interface{}) error {
	return c.request(ctx, http.MethodPut, endpoint, payload, out)
}

func (c *Client) delete(ctx context.Context, endpoint string) error {
	return c.request(ctx, http.MethodDelete, endpoint, nil, nil)
}

func (c *Client) request(ctx context.Context, method, endpoint string, payload any, out interface{}) error {
	var rd io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("bitbucket API %s: %s", endpoint, resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// rawGet fetches a URL and returns the body as text. It is used for endpoints
// that return a patch rather than JSON, such as a pull request diff.
func (c *Client) rawGet(ctx context.Context, endpoint, accept string) (string, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, err
	}
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Accept", accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return "", resp, fmt.Errorf("bitbucket API %s: %s", endpoint, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp, err
	}
	return string(b), resp, nil
}

// runIDFromUUID returns a stable numeric-ish identifier from a Bitbucket
// pipeline UUID (e.g. "{abc123...}"), falling back to 0 when unparsable.
func runIDFromUUID(uuid string) int64 {
	// Bitbucket pipeline UUIDs are not numeric; use a stable FNV hash so the
	// id is a consistent positive int for display and matching.
	h := fnv.New64a()
	h.Write([]byte(uuid))
	return int64(h.Sum64())
}

// ListIssues reports that Bitbucket no longer provides an issue tracker.
// The tracker was retired in 2023 and the issue API endpoints are marked
// deprecated; scanning them would silently return empty or stale data.
func (c *Client) ListIssues(ctx context.Context) ([]operations.Issue, error) {
	return nil, fmt.Errorf("%w: Bitbucket retired its issue tracker in 2023; pull requests are still available via 'forge prs'", operations.ErrIssuesUnsupported)
}

// ListPRs returns open pull requests across the workspace's repositories.
func (c *Client) ListPRs(ctx context.Context) ([]operations.PR, error) {
	repos, err := c.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	if len(repos) > maxPRRepos {
		repos = repos[:maxPRRepos]
	}
	var out []operations.PR
	skipped := 0
	for _, r := range repos {
		var body struct {
			Values []prItem `json:"values"`
		}
		u := fmt.Sprintf("%s/repositories/%s/pullrequests?pagelen=50&state=OPEN", apiBase, fullNamePath(r.FullName))
		if err := c.get(ctx, u, &body); err != nil {
			skipped++
			continue
		}
		for _, pr := range body.Values {
			out = append(out, operations.PR{
				Forge:    c.forgeLabelOrDefault(),
				Instance: c.Name(),
				Repo:     r.FullName,
				Number:   int(pr.ID),
				Title:    pr.Title,
				State:    pr.State,
				URL:      pr.Links.HTML.Href,
			})
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "warning: %s: skipped %d repository(ies)\n", c.Name(), skipped)
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("bitbucket API %s: %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func fullNamePath(full string) string {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) == 2 {
		return url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	}
	return url.PathEscape(full)
}
