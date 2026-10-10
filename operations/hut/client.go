// Package hut provides access to SourceHut (git.sr.ht, todo.sr.ht and
// lists.sr.ht) via their GraphQL APIs.
//
// SourceHut removed its legacy REST APIs in late 2025; every service now
// exposes a single GraphQL endpoint (/query) authenticated with an OAuth 2.0
// bearer token. The username is resolved from the token (the `me` query), so
// it is no longer required in the config.
package hut

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pratyay360/forgit/operations"
)

var (
	gitAPIBase   = "https://git.sr.ht/query"
	todoAPIBase  = "https://todo.sr.ht/query"
	listsAPIBase = "https://lists.sr.ht/query"
)

// maxPages bounds every pagination loop so a server that keeps returning a
// non-nil cursor cannot hang the command for the whole request timeout.
const maxPages = 100

// errStuckCursor is returned when a pagination loop exceeds maxPages.
var errStuckCursor = fmt.Errorf("server kept returning a cursor (more than %d pages)", maxPages)

type Client struct {
	token      string
	http       *http.Client
	instance   string
	forgeLabel string // user-visible forge name; defaults to "sourcehut"
}

// New builds a SourceHut client from the given credentials.
func New(cfg operations.SourceHutConfig) (*Client, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("sourcehut: token is required (set [sourcehut] token in config or %s)", operations.EnvSourceHutToken)
	}
	return &Client{
		token:      cfg.Token,
		http:       &http.Client{Timeout: 30 * time.Second},
		forgeLabel: "sourcehut",
	}, nil
}

// SetInstance names this client's instance (default: the forge type).
func (c *Client) SetInstance(name string) { c.instance = name }

// Name returns the instance name.
func (c *Client) Name() string {
	if c.instance != "" {
		return c.instance
	}
	return "sourcehut"
}

// SetForgeLabel overrides the forge label the client stamps on returned
// resources. Used to give user-named aliases the right identity in listings
// while the SourceHut client does the API work.
func (c *Client) SetForgeLabel(label string) {
	if label != "" {
		c.forgeLabel = label
	}
}

// forgeLabelOrDefault returns the label to stamp on returned resources,
// falling back to "sourcehut" when SetForgeLabel was never called.
func (c *Client) forgeLabelOrDefault() string {
	if c.forgeLabel != "" {
		return c.forgeLabel
	}
	return "sourcehut"
}

// gqlRequest is a GraphQL POST body.
type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// gqlResponse is a standard GraphQL envelope.
type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// query posts a GraphQL query to endpoint and decodes the data object into
// out.
func (c *Client) query(ctx context.Context, endpoint string, req gqlRequest, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("User-Agent", "forge/0.1")
	resp, err := c.http.Do(r)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var gql gqlResponse
	// SourceHut reports API and auth problems as 4xx responses carrying a
	// GraphQL errors envelope; surface the message (e.g. "Invalid OAuth
	// bearer token") instead of a bare status line when possible.
	if err := json.Unmarshal(raw, &gql); err != nil {
		if resp.StatusCode >= 400 {
			return fmt.Errorf("sourcehut %s: %s", endpoint, resp.Status)
		}
		return fmt.Errorf("sourcehut %s: decoding response: %w", endpoint, err)
	}
	if len(gql.Errors) > 0 {
		msgs := make([]string, 0, len(gql.Errors))
		for _, e := range gql.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("sourcehut: %s", strings.Join(msgs, "; "))
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("sourcehut %s: %s", endpoint, resp.Status)
	}
	if gql.Data != nil {
		return json.Unmarshal(gql.Data, out)
	}
	return nil
}

// stripTilde normalizes a canonical name like "~sircmpwn" to "sircmpwn".
func stripTilde(s string) string { return strings.TrimPrefix(s, "~") }

const repositoriesQuery = `query($cursor: Cursor) {
  me {
    username
    repositories(cursor: $cursor) {
      results { name visibility }
      cursor
    }
  }
}`

// ListRepos returns the authenticated user's git.sr.ht repositories.
func (c *Client) ListRepos(ctx context.Context) ([]operations.Repo, error) {
	var out []operations.Repo
	username := ""
	var cursor *string
	pages := 0
	for {
		pages++
		if pages > maxPages {
			return nil, fmt.Errorf("listing repositories: %w", errStuckCursor)
		}
		var page struct {
			Me struct {
				Username     string `json:"username"`
				Repositories struct {
					Results []struct {
						Name       string `json:"name"`
						Visibility string `json:"visibility"`
					} `json:"results"`
					Cursor *string `json:"cursor"`
				} `json:"repositories"`
			} `json:"me"`
		}
		if err := c.query(ctx, gitAPIBase, gqlRequest{
			Query:     repositoriesQuery,
			Variables: map[string]any{"cursor": cursor},
		}, &page); err != nil {
			return nil, fmt.Errorf("listing repositories: %w", err)
		}
		if username == "" {
			username = stripTilde(page.Me.Username)
		}
		for _, r := range page.Me.Repositories.Results {
			out = append(out, operations.Repo{
				Forge:    c.forgeLabelOrDefault(),
				Instance: c.Name(),
				FullName: username + "/" + r.Name,
				URL:      "https://git.sr.ht/~" + username + "/" + r.Name,
				Private:  r.Visibility == "PRIVATE",
			})
		}
		cursor = page.Me.Repositories.Cursor
		if cursor == nil {
			return out, nil
		}
	}
}

const createRepoMutation = `mutation($name: String!, $visibility: Visibility!, $description: String) {
  createRepository(name: $name, visibility: $visibility, description: $description) {
    id name visibility
  }
}`

// CreateRepo creates a git.sr.ht repository owned by the authenticated user.
func (c *Client) CreateRepo(ctx context.Context, in operations.RepoInput) (operations.Repo, error) {
	visibility := "PUBLIC"
	if in.Private {
		visibility = "PRIVATE"
	}
	var result struct {
		CreateRepository struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Visibility string `json:"visibility"`
		} `json:"createRepository"`
	}
	username, err := c.meUsername(ctx)
	if err != nil {
		return operations.Repo{}, fmt.Errorf("creating repository: %w", err)
	}
	var desc any
	if in.Description != "" {
		desc = in.Description
	}
	if err := c.query(ctx, gitAPIBase, gqlRequest{
		Query: createRepoMutation,
		Variables: map[string]any{
			"name":        in.Name,
			"visibility":  visibility,
			"description": desc,
		},
	}, &result); err != nil {
		return operations.Repo{}, fmt.Errorf("creating repository: %w", err)
	}
	name := result.CreateRepository.Name
	return operations.Repo{
		Forge:    c.forgeLabelOrDefault(),
		Instance: c.Name(),
		FullName: username + "/" + name,
		URL:      "https://git.sr.ht/~" + username + "/" + name,
		Private:  result.CreateRepository.Visibility == "PRIVATE",
	}, nil
}

const updateRepoMutation = `mutation($id: Int!, $input: RepoInput!) {
  updateRepository(id: $id, input: $input) {
    id name visibility
  }
}`

// RenameRepo renames a repository. fullName is user/name.
func (c *Client) RenameRepo(ctx context.Context, fullName, newName string) (operations.Repo, error) {
	return c.updateRepo(ctx, fullName, map[string]any{"name": newName})
}

// SetVisibility changes whether a repository is private. fullName is user/name.
func (c *Client) SetVisibility(ctx context.Context, fullName string, private bool) (operations.Repo, error) {
	visibility := "PUBLIC"
	if private {
		visibility = "PRIVATE"
	}
	return c.updateRepo(ctx, fullName, map[string]any{"visibility": visibility})
}

// updateRepo applies the given extra input fields to a repository via the
// updateRepository mutation. extra holds RepoInput fields (name, visibility,
// description, ...).
func (c *Client) updateRepo(ctx context.Context, fullName string, extra map[string]any) (operations.Repo, error) {
	owner, name, ok := splitHutName(fullName)
	if !ok {
		return operations.Repo{}, fmt.Errorf("invalid repository name %q", fullName)
	}
	id, err := c.repositoryID(ctx, owner, name)
	if err != nil {
		return operations.Repo{}, fmt.Errorf("updating repository: %w", err)
	}
	var result struct {
		UpdateRepository struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Visibility string `json:"visibility"`
		} `json:"updateRepository"`
	}
	if err := c.query(ctx, gitAPIBase, gqlRequest{
		Query:     updateRepoMutation,
		Variables: map[string]any{"id": id, "input": extra},
	}, &result); err != nil {
		return operations.Repo{}, fmt.Errorf("updating repository: %w", err)
	}
	repoName := result.UpdateRepository.Name
	return operations.Repo{
		Forge:    c.forgeLabelOrDefault(),
		Instance: c.Name(),
		FullName: owner + "/" + repoName,
		URL:      "https://git.sr.ht/~" + owner + "/" + repoName,
		Private:  result.UpdateRepository.Visibility == "PRIVATE",
	}, nil
}

// repositoryIDQuery fetches a repository ID by owner and name. User.repository
// requires the owner's canonical name ("~user").
const repositoryIDQuery = `query($username: String!, $name: String!) {
  user(username: $username) {
    repository(name: $name) { id name visibility }
  }
}`

// repositoryID resolves the numeric repository ID required by the
// updateRepository and deleteRepository mutations.
func (c *Client) repositoryID(ctx context.Context, owner, name string) (int, error) {
	var result struct {
		User struct {
			Repository *struct {
				ID int `json:"id"`
			} `json:"repository"`
		} `json:"user"`
	}
	// The user(username:) argument takes the canonical "~user" form; fall
	// back to the bare name if the canonical lookup misses.
	for _, username := range []string{"~" + owner, owner} {
		result.User.Repository = nil
		if err := c.query(ctx, gitAPIBase, gqlRequest{
			Query:     repositoryIDQuery,
			Variables: map[string]any{"username": username, "name": name},
		}, &result); err != nil {
			return 0, err
		}
		if result.User.Repository != nil {
			return result.User.Repository.ID, nil
		}
	}
	return 0, fmt.Errorf("repository %q not found", owner+"/"+name)
}

const deleteRepoMutation = `mutation($id: Int!) {
  deleteRepository(id: $id) { id }
}`

// DeleteRepo deletes a repository. fullName is user/name.
func (c *Client) DeleteRepo(ctx context.Context, fullName string) error {
	owner, name, ok := splitHutName(fullName)
	if !ok {
		return fmt.Errorf("invalid repository name %q", fullName)
	}
	id, err := c.repositoryID(ctx, owner, name)
	if err != nil {
		return fmt.Errorf("deleting repository: %w", err)
	}
	var result struct {
		DeleteRepository struct {
			ID int `json:"id"`
		} `json:"deleteRepository"`
	}
	if err := c.query(ctx, gitAPIBase, gqlRequest{
		Query:     deleteRepoMutation,
		Variables: map[string]any{"id": id},
	}, &result); err != nil {
		return fmt.Errorf("deleting repository: %w", err)
	}
	return nil
}

// buildsAPIBase is the builds.sr.ht GraphQL endpoint.
var buildsAPIBase = "https://builds.sr.ht/query"

const jobsQuery = `query($cursor: Cursor) {
  me {
    username
    jobs(cursor: $cursor) {
      results { id status note tags }
      cursor
    }
  }
}`

// ListRuns returns builds.sr.ht jobs associated with a repository.
// fullName is user/name.
//
// builds.sr.ht jobs are owned by a user, not by a repository: the GraphQL API
// offers no per-repository job query. Jobs submitted by git.sr.ht carry the
// repository name as one of their tags, so the authenticated user's jobs are
// listed and filtered on that tag. Manually submitted jobs that do not tag the
// repository are therefore not reported.
func (c *Client) ListRuns(ctx context.Context, fullName string) ([]operations.Run, error) {
	_, name, ok := splitHutName(fullName)
	if !ok {
		return nil, fmt.Errorf("invalid repository name %q", fullName)
	}
	var out []operations.Run
	var cursor *string
	username := ""
	pages := 0
	for {
		pages++
		if pages > maxPages {
			return nil, fmt.Errorf("listing builds: %w", errStuckCursor)
		}
		var page struct {
			Me struct {
				Username string `json:"username"`
				Jobs     struct {
					Results []struct {
						ID     int64    `json:"id"`
						Status string   `json:"status"`
						Note   string   `json:"note"`
						Tags   []string `json:"tags"`
					} `json:"results"`
					Cursor *string `json:"cursor"`
				} `json:"jobs"`
			} `json:"me"`
		}
		if err := c.query(ctx, buildsAPIBase, gqlRequest{
			Query:     jobsQuery,
			Variables: map[string]any{"cursor": cursor},
		}, &page); err != nil {
			return nil, fmt.Errorf("listing builds: %w", err)
		}
		if username == "" {
			username = stripTilde(page.Me.Username)
		}
		for _, j := range page.Me.Jobs.Results {
			if !taggedWith(j.Tags, name) {
				continue
			}
			out = append(out, operations.Run{
				Forge:    c.forgeLabelOrDefault(),
				Instance: c.Name(),
				Repo:     fullName,
				ID:       j.ID,
				Name:     jobName(j.Note, j.Tags),
				Status:   strings.ToLower(j.Status),
				URL:      fmt.Sprintf("https://builds.sr.ht/~%s/job/%d", username, j.ID),
			})
		}
		cursor = page.Me.Jobs.Cursor
		if cursor == nil {
			return out, nil
		}
	}
}

// taggedWith reports whether any tag matches the repository name. Tags are
// compared case-sensitively, as sourcehut repository names are.
func taggedWith(tags []string, name string) bool {
	for _, t := range tags {
		if t == name {
			return true
		}
	}
	return false
}

// jobName labels a build from its note (first line) or, failing that, its tags.
func jobName(note string, tags []string) string {
	if line := strings.TrimSpace(strings.SplitN(note, "\n", 2)[0]); line != "" {
		return line
	}
	return strings.Join(tags, "/")
}

// ListWorkflows returns ErrNotSupported: sourcehut has no workflow-definition
// concept. Build manifests (.build.yml) are submitted per job rather than
// registered on the repository, so there is nothing to enumerate; the jobs
// themselves are reported by ListRuns.
func (c *Client) ListWorkflows(ctx context.Context, fullName string) ([]operations.Workflow, error) {
	return nil, fmt.Errorf("%w: sourcehut has no workflow definitions; see runs (builds)", operations.ErrNotSupported)
}

// ListProjects returns ErrNotSupported: sourcehut has no project concept
// equivalent to GitHub/GitLab projects.
func (c *Client) ListProjects(ctx context.Context) ([]operations.Project, error) {
	return nil, fmt.Errorf("%w: sourcehut has no projects concept", operations.ErrNotSupported)
}

// meUsername resolves the authenticated user's (unprefixed) username.
func (c *Client) meUsername(ctx context.Context) (string, error) {
	var page struct {
		Me struct {
			Username string `json:"username"`
		} `json:"me"`
	}
	if err := c.query(ctx, gitAPIBase, gqlRequest{
		Query: `query { me { username } }`,
	}, &page); err != nil {
		return "", err
	}
	return stripTilde(page.Me.Username), nil
}

// splitHutName splits a "user/name" repository reference into its parts.
func splitHutName(full string) (owner, name string, ok bool) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return stripTilde(parts[0]), parts[1], true
}

const trackersQuery = `query($cursor: Cursor) {
  me {
    username
    trackers(cursor: $cursor) {
      results { name }
      cursor
    }
  }
}`

const ticketsQuery = `query($name: String!, $cursor: Cursor) {
  me {
    tracker(name: $name) {
      tickets(cursor: $cursor) {
        results { id ref subject status }
        cursor
      }
    }
  }
}`

// ticket mirrors the GraphQL Ticket object of todo.sr.ht.
type ticket struct {
	ID      int64  `json:"id"`
	Ref     string `json:"ref"`
	Subject string `json:"subject"`
	Status  string `json:"status"`
}

// ListIssues returns open tickets across the authenticated user's trackers.
func (c *Client) ListIssues(ctx context.Context) ([]operations.Issue, error) {
	names, username, err := c.trackerNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing trackers: %w", err)
	}
	var out []operations.Issue
	for _, name := range names {
		tickets, err := c.tickets(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("listing tickets for %s: %w", name, err)
		}
		for _, tk := range tickets {
			if !openTicketStatus(tk.Status) {
				continue
			}
			// The schema describes ref as a canonical reference string; it may
			// carry a path (e.g. ~user/tracker/123). Keep only the last segment
			// so the number and URL stay well-formed either way.
			ref := tk.Ref
			if i := strings.LastIndex(ref, "/"); i >= 0 {
				ref = ref[i+1:]
			}
			if ref == "" {
				ref = strconv.FormatInt(tk.ID, 10)
			}
			number := int(tk.ID)
			if n, err := strconv.Atoi(ref); err == nil {
				number = n
			}
			out = append(out, operations.Issue{
				Forge:    c.forgeLabelOrDefault(),
				Instance: c.Name(),
				Repo:     name,
				Number:   number,
				Title:    tk.Subject,
				State:    strings.ToLower(tk.Status),
				URL:      fmt.Sprintf("https://todo.sr.ht/~%s/%s/%s", username, name, ref),
			})
		}
	}
	return out, nil
}

// openTicketStatus reports whether a todo.sr.ht ticket status is open.
// RESOLVED is the only closed status; REPORTED/CONFIRMED/IN_PROGRESS/PENDING
// are all work in progress.
func openTicketStatus(s string) bool {
	switch s {
	case "REPORTED", "CONFIRMED", "IN_PROGRESS", "PENDING":
		return true
	}
	return false
}

func (c *Client) trackerNames(ctx context.Context) (names []string, username string, err error) {
	var cursor *string
	pages := 0
	for {
		pages++
		if pages > maxPages {
			return nil, "", errStuckCursor
		}
		var page struct {
			Me struct {
				Username string `json:"username"`
				Trackers struct {
					Results []struct {
						Name string `json:"name"`
					} `json:"results"`
					Cursor *string `json:"cursor"`
				} `json:"trackers"`
			} `json:"me"`
		}
		if err := c.query(ctx, todoAPIBase, gqlRequest{
			Query:     trackersQuery,
			Variables: map[string]any{"cursor": cursor},
		}, &page); err != nil {
			return nil, "", err
		}
		if username == "" {
			username = stripTilde(page.Me.Username)
		}
		for _, t := range page.Me.Trackers.Results {
			names = append(names, t.Name)
		}
		cursor = page.Me.Trackers.Cursor
		if cursor == nil {
			return names, username, nil
		}
	}
}

func (c *Client) tickets(ctx context.Context, trackerName string) ([]ticket, error) {
	var out []ticket
	var cursor *string
	pages := 0
	for {
		pages++
		if pages > maxPages {
			return nil, errStuckCursor
		}
		var page struct {
			Me struct {
				Tracker struct {
					Tickets struct {
						Results []ticket `json:"results"`
						Cursor  *string  `json:"cursor"`
					} `json:"tickets"`
				} `json:"tracker"`
			} `json:"me"`
		}
		if err := c.query(ctx, todoAPIBase, gqlRequest{
			Query:     ticketsQuery,
			Variables: map[string]any{"name": trackerName, "cursor": cursor},
		}, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Me.Tracker.Tickets.Results...)
		cursor = page.Me.Tracker.Tickets.Cursor
		if cursor == nil {
			return out, nil
		}
	}
}

const listsQuery = `query($cursor: Cursor) {
  me {
    username
    lists(cursor: $cursor) {
      results { name }
      cursor
    }
  }
}`

const patchsetsQuery = `query($name: String!, $cursor: Cursor) {
  me {
    list(name: $name) {
      patches(cursor: $cursor) {
        results { id subject status }
        cursor
      }
    }
  }
}`

// patchset mirrors the GraphQL Patchset object of lists.sr.ht.
type patchset struct {
	ID      int64  `json:"id"`
	Subject string `json:"subject"`
	Status  string `json:"status"`
}

// ListPRs returns open patchsets across the authenticated user's mailing
// lists.
func (c *Client) ListPRs(ctx context.Context) ([]operations.PR, error) {
	names, username, err := c.listNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing mailing lists: %w", err)
	}
	var out []operations.PR
	for _, name := range names {
		patchsets, err := c.patchsets(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("listing patchsets for %s: %w", name, err)
		}
		for _, ps := range patchsets {
			if !openPatchsetStatus(ps.Status) {
				continue
			}
			out = append(out, operations.PR{
				Forge:    c.forgeLabelOrDefault(),
				Instance: c.Name(),
				Repo:     name,
				Number:   int(ps.ID),
				Title:    ps.Subject,
				State:    strings.ToLower(ps.Status),
				URL:      fmt.Sprintf("https://lists.sr.ht/~%s/%s/patches/%d", username, name, ps.ID),
			})
		}
	}
	return out, nil
}

// openPatchsetStatus reports whether a lists.sr.ht patchset is still in
// review. PROPOSED and NEEDS_REVISION are active; UNKNOWN statuses (legacy
// patches without a recorded status) are conservatively treated as open
// rather than silently dropped. SUPERSEDED/APPROVED/REJECTED/APPLIED are
// terminal.
func openPatchsetStatus(s string) bool {
	switch s {
	case "PROPOSED", "NEEDS_REVISION", "UNKNOWN":
		return true
	}
	return false
}

func (c *Client) listNames(ctx context.Context) (names []string, username string, err error) {
	var cursor *string
	pages := 0
	for {
		pages++
		if pages > maxPages {
			return nil, "", errStuckCursor
		}
		var page struct {
			Me struct {
				Username string `json:"username"`
				Lists    struct {
					Results []struct {
						Name string `json:"name"`
					} `json:"results"`
					Cursor *string `json:"cursor"`
				} `json:"lists"`
			} `json:"me"`
		}
		if err := c.query(ctx, listsAPIBase, gqlRequest{
			Query:     listsQuery,
			Variables: map[string]any{"cursor": cursor},
		}, &page); err != nil {
			return nil, "", err
		}
		if username == "" {
			username = stripTilde(page.Me.Username)
		}
		for _, l := range page.Me.Lists.Results {
			names = append(names, l.Name)
		}
		cursor = page.Me.Lists.Cursor
		if cursor == nil {
			return names, username, nil
		}
	}
}

func (c *Client) patchsets(ctx context.Context, listName string) ([]patchset, error) {
	var out []patchset
	var cursor *string
	pages := 0
	for {
		pages++
		if pages > maxPages {
			return nil, errStuckCursor
		}
		var page struct {
			Me struct {
				List struct {
					Patches struct {
						Results []patchset `json:"results"`
						Cursor  *string    `json:"cursor"`
					} `json:"patches"`
				} `json:"list"`
			} `json:"me"`
		}
		if err := c.query(ctx, listsAPIBase, gqlRequest{
			Query:     patchsetsQuery,
			Variables: map[string]any{"name": listName, "cursor": cursor},
		}, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Me.List.Patches.Results...)
		cursor = page.Me.List.Patches.Cursor
		if cursor == nil {
			return out, nil
		}
	}
}
