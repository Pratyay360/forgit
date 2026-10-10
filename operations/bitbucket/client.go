// Package bitbucket provides access to Bitbucket Cloud via its REST API v2.
//
// Two credential styles are supported. User API tokens and app passwords
// REQUIRE your Atlassian account email plus the token, sent as HTTP Basic
// auth. Bearer auth ("Authorization: Bearer <token>") only works for OAuth
// tokens and workspace/project/repository access tokens; sending a user API
// token as Bearer fails with 401 "API token must be used with an atlassian
// registered email". So always set username for API tokens/app passwords.
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
	workspace  string
	http       *http.Client
	instance   string
	// wsCache memoizes workspace discovery for the client's lifetime so
	// multi-repo commands (e.g. listing pull requests) discover once.
	wsCache    []string
	wsCached   bool
	forgeLabel string // user-visible forge name; defaults to "bitbucket"
}

// New builds a Bitbucket client from the given credentials. Only the token
// is required to construct the client, but user API tokens and app passwords
// must be paired with the Atlassian account email as username (Basic auth) —
// without it requests fall back to Bearer auth and Bitbucket rejects API
// tokens with 401 "API token must be used with an atlassian registered
// email". Bearer-only is only valid for OAuth and workspace/project/
// repository access tokens.
func New(cfg operations.BitbucketConfig) (*Client, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("bitbucket: token is required (set [bitbucket] token in config or %s)", operations.EnvBitbucketToken)
	}
	c := &Client{
		username:   cfg.Username,
		token:      cfg.Token,
		workspace:  cfg.Workspace,
		http:       &http.Client{Timeout: 30 * time.Second},
		forgeLabel: "bitbucket",
	}
	if cfg.Workspace != "" {
		c.addWorkspace(cfg.Workspace)
	}
	return c, nil
}

// setAuth applies the configured credentials to a request: Basic auth when a
// username (Atlassian email) is configured, Bearer auth otherwise. User API
// tokens and app passwords must use Basic auth — see New.
func (c *Client) setAuth(req *http.Request) {
	if c.username != "" {
		req.SetBasicAuth(c.username, c.token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
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
	Workspace struct {
		Slug string `json:"slug"`
	} `json:"workspace"`
}

type prItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
	Links     struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

// workspaceItem is one entry of GET /user/permissions/workspaces.
type workspaceItem struct {
	Workspace struct {
		Slug string `json:"slug"`
	} `json:"workspace"`
}

func (c *Client) addWorkspace(slug string) {
	if slug == "" || strings.Contains(slug, "@") {
		return
	}
	for _, w := range c.wsCache {
		if w == slug {
			return
		}
	}
	c.wsCache = append(c.wsCache, slug)
}

// getAll follows Bitbucket's paginated {values, next} envelope until `next`
// is absent, collecting every page's values.
func getAll[T any](ctx context.Context, c *Client, start string) ([]T, error) {
	var all []T
	for next := start; next != ""; {
		var page struct {
			Values []T    `json:"values"`
			Next   string `json:"next"`
		}
		if err := c.get(ctx, next, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Values...)
		next = page.Next
	}
	return all, nil
}

// workspaces returns the slugs of every workspace the token can access,
// caching the result for the client's lifetime.
func (c *Client) workspaces(ctx context.Context) ([]string, error) {
	if c.wsCached && len(c.wsCache) > 0 {
		return c.wsCache, nil
	}

	items, err := getAll[workspaceItem](ctx, c, fmt.Sprintf("%s/user/permissions/workspaces?pagelen=100", apiBase))
	if err == nil && len(items) > 0 {
		c.wsCached = true
		for _, it := range items {
			c.addWorkspace(it.Workspace.Slug)
		}
		if len(c.wsCache) > 0 {
			return c.wsCache, nil
		}
	}

	// Try GET /user to get user.username as personal workspace
	var user struct {
		Username string `json:"username"`
	}
	if uErr := c.get(ctx, fmt.Sprintf("%s/user", apiBase), &user); uErr == nil && user.Username != "" {
		c.wsCached = true
		c.addWorkspace(user.Username)
		return c.wsCache, nil
	}

	if c.workspace != "" {
		c.wsCached = true
		c.addWorkspace(c.workspace)
		return c.wsCache, nil
	}

	// Never use an email address as a workspace slug
	if c.username != "" && !strings.Contains(c.username, "@") {
		c.wsCached = true
		c.addWorkspace(c.username)
		return c.wsCache, nil
	}

	if len(c.wsCache) > 0 {
		c.wsCached = true
		return c.wsCache, nil
	}

	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no accessible workspace found")
}

// defaultWorkspace resolves which workspace a new repository belongs to: an
// explicit --owner always wins, an explicitly configured workspace is next,
// a single accessible workspace is used automatically, and anything
// ambiguous asks the user to pass --owner.
func (c *Client) defaultWorkspace(ctx context.Context, owner string) (string, error) {
	if owner != "" {
		return owner, nil
	}
	if c.workspace != "" {
		return c.workspace, nil
	}
	ws, err := c.workspaces(ctx)
	if err != nil {
		return "", err
	}
	switch len(ws) {
	case 1:
		return ws[0], nil
	case 0:
		return "", fmt.Errorf("creating repository: no accessible workspace found; pass --owner <workspace>")
	default:
		return "", fmt.Errorf("creating repository: multiple accessible workspaces (%s); pass --owner <workspace> to pick one", strings.Join(ws, ", "))
	}
}

// slugify derives a Bitbucket repository slug from a display name:
// lowercase, with runs of characters outside [a-z0-9-_] collapsed to a
// single hyphen. The display name itself is sent as the repo's name.
func slugify(name string) string {
	var b strings.Builder
	prevDash := true // start true to trim leading separators
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_' || r == '-':
			if r == '-' && prevDash {
				continue
			}
			b.WriteRune(r)
			prevDash = r == '-'
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	if s := strings.Trim(b.String(), "-"); s != "" {
		return s
	}
	return "repo"
}

// ListRepos returns the repositories accessible to the configured credentials.
// It queries Bitbucket's global repositories endpoint (/2.0/repositories?role=member),
// which returns repositories across all workspaces the user belongs to.
// If that endpoint fails, it falls back to querying workspaces individually.
func (c *Client) ListRepos(ctx context.Context) ([]operations.Repo, error) {
	u := fmt.Sprintf("%s/repositories?pagelen=100&role=member", apiBase)
	items, err := getAll[repoItem](ctx, c, u)
	if err == nil {
		out := make([]operations.Repo, 0, len(items))
		for _, r := range items {
			if r.Workspace.Slug != "" {
				c.addWorkspace(r.Workspace.Slug)
			}
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

	// Fallback for workspace-scoped access tokens or configurations restricting global listing
	ws, wsErr := c.workspaces(ctx)
	if wsErr != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	var out []operations.Repo
	for _, w := range ws {
		wsURL := fmt.Sprintf("%s/repositories/%s?pagelen=100", apiBase, url.PathEscape(w))
		wsItems, err := getAll[repoItem](ctx, c, wsURL)
		if err != nil {
			return nil, fmt.Errorf("listing repositories for workspace %s: %w", w, err)
		}
		for _, r := range wsItems {
			if r.Workspace.Slug != "" {
				c.addWorkspace(r.Workspace.Slug)
			}
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
	}
	return out, nil
}

// CreateRepo creates a repository in the given workspace (--owner), or the
// single accessible workspace when unambiguous.
func (c *Client) CreateRepo(ctx context.Context, in operations.RepoInput) (operations.Repo, error) {
	ws, err := c.defaultWorkspace(ctx, in.Owner)
	if err != nil {
		return operations.Repo{}, err
	}
	slug := slugify(in.Name)
	var body struct {
		FullName  string `json:"full_name"`
		IsPrivate bool   `json:"is_private"`
		Links     struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	payload := map[string]any{
		"name":       in.Name,
		"scm":        "git",
		"is_private": in.Private,
	}
	if in.Description != "" {
		payload["description"] = in.Description
	}
	u := fmt.Sprintf("%s/repositories/%s/%s", apiBase, url.PathEscape(ws), url.PathEscape(slug))
	if err := c.post(ctx, u, payload, &body); err != nil {
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

// pipelineItem is one entry of a repository's pipelines listing.
type pipelineItem struct {
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
}

// ListRuns lists pipeline runs for a repository. fullName is workspace/repo.
func (c *Client) ListRuns(ctx context.Context, fullName string) ([]operations.Run, error) {
	u := fmt.Sprintf("%s/repositories/%s/pipelines/?pagelen=50", apiBase, fullNamePath(fullName))
	items, err := getAll[pipelineItem](ctx, c, u)
	if err != nil {
		return nil, fmt.Errorf("listing pipelines: %w", err)
	}
	out := make([]operations.Run, 0, len(items))
	for _, p := range items {
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

// projectItem is one entry of a workspace's projects listing.
type projectItem struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

// ListProjects lists projects across every workspace the token can access.
func (c *Client) ListProjects(ctx context.Context) ([]operations.Project, error) {
	ws, err := c.workspaces(ctx)
	if err != nil {
		return nil, err
	}
	if len(ws) == 0 && c.username != "" {
		ws = []string{c.username}
	}
	var out []operations.Project
	for _, w := range ws {
		u := fmt.Sprintf("%s/workspaces/%s/projects?pagelen=100", apiBase, url.PathEscape(w))
		items, err := getAll[projectItem](ctx, c, u)
		if err != nil {
			return nil, fmt.Errorf("listing projects for workspace %s: %w", w, err)
		}
		for _, p := range items {
			out = append(out, operations.Project{
				Forge:    c.forgeLabelOrDefault(),
				Instance: c.Name(),
				FullName: p.Key,
				URL:      p.Links.HTML.Href,
			})
		}
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
	c.setAuth(req)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return apiError(method, endpoint, resp.Status, raw)
	}
	if out != nil {
		if len(raw) == 0 {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	return nil
}

// apiError builds an error from a failed API response, surfacing the server's
// message when the body carries one (Bitbucket returns
// {"error": {"message": ...}}). When Bitbucket rejects a Bearer-sent API
// token because it needs an Atlassian email, or rejects a token without Bitbucket
// scopes, the hint guides the user on how to configure valid credentials.
func apiError(method, endpoint, status string, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var parsed struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
			Detail  string `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Error.Message != "" {
			msg = parsed.Error.Message
			if parsed.Error.Detail != "" {
				msg += ": " + parsed.Error.Detail
			}
		} else if parsed.Message != "" {
			msg = parsed.Message
		}
	} else if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if msg == "" {
		msg = status
	} else {
		msg = status + ": " + msg
	}
	err := fmt.Errorf("bitbucket API %s %s: %s", method, endpoint, msg)
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "must be used with") && strings.Contains(lower, "email") {
		err = fmt.Errorf("%w (API tokens/app passwords need Basic auth: set [bitbucket] username to your Atlassian account email or %s)", err, operations.EnvBitbucketUsername)
	}
	if strings.Contains(lower, "no bitbucket scopes") {
		err = fmt.Errorf("%w (your Atlassian API token needs Bitbucket scopes: go to https://id.atlassian.com/manage-profile/security/api-tokens, create an API token with scopes, and check the Bitbucket scopes like read:repository:bitbucket)", err)
	}
	return err
}

// rawGet fetches a URL and returns the body as text. It is used for endpoints
// that return a patch rather than JSON, such as a pull request diff.
func (c *Client) rawGet(ctx context.Context, endpoint, accept string) (string, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, err
	}
	c.setAuth(req)
	req.Header.Set("Accept", accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp, err
	}
	if resp.StatusCode >= 400 {
		return "", resp, apiError(http.MethodGet, endpoint, resp.Status, b)
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
		u := fmt.Sprintf("%s/repositories/%s/pullrequests?pagelen=50&state=OPEN", apiBase, fullNamePath(r.FullName))
		prs, err := getAll[prItem](ctx, c, u)
		if err != nil {
			skipped++
			continue
		}
		for _, pr := range prs {
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
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return apiError(http.MethodGet, endpoint, resp.Status, raw)
	}
	return json.Unmarshal(raw, out)
}

func fullNamePath(full string) string {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) == 2 {
		return url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	}
	return url.PathEscape(full)
}
