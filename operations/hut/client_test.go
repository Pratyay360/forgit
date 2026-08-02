package hut

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pratyay360/forge/v1/operations"
)

// writeGQL writes a GraphQL success envelope with the given data object.
func writeGQL(w http.ResponseWriter, data map[string]any) {
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// gqlQuery extracts the query string from a GraphQL request body.
func gqlQuery(t *testing.T, r *http.Request) string {
	t.Helper()
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decoding graphql request: %v", err)
	}
	return req.Query
}

// wantPath fails the test unless the request hit the given service prefix.
func wantPath(t *testing.T, r *http.Request, prefix string) {
	t.Helper()
	if !strings.HasPrefix(r.URL.Path, prefix) {
		t.Errorf("request path = %q, want prefix %q", r.URL.Path, prefix)
	}
}

// newTestClient starts a fake SourceHut GraphQL endpoint and returns a
// client pointed at it. Each service base points at a distinct path prefix
// (/git, /todo, /lists) so tests can assert queries hit the right service.
//
// Note: gitAPIBase/todoAPIBase/listsAPIBase are package-level test seams;
// these tests must not run in parallel with each other or with other tests
// that touch them.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	oldGit, oldTodo, oldLists := gitAPIBase, todoAPIBase, listsAPIBase
	gitAPIBase, todoAPIBase, listsAPIBase = ts.URL+"/git", ts.URL+"/todo", ts.URL+"/lists"
	t.Cleanup(func() {
		gitAPIBase, todoAPIBase, listsAPIBase = oldGit, oldTodo, oldLists
	})

	c, err := New(operations.SourceHutConfig{Token: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// checkAuth asserts the request carries the expected bearer token and a
// User-Agent header (the SourceHut API rejects missing auth/user-agent).
func checkAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer secret")
	}
	if r.Header.Get("User-Agent") == "" {
		t.Error("expected a User-Agent header on sourcehut requests")
	}
}

func TestListRepos(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		wantPath(t, r, "/git")
		q := gqlQuery(t, r)
		if !strings.Contains(q, "repositories") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeGQL(w, map[string]any{
			"me": map[string]any{
				"username": "alice",
				"repositories": map[string]any{
					"results": []map[string]any{
						{"name": "alpha", "visibility": "PUBLIC"},
						{"name": "vault", "visibility": "PRIVATE"},
					},
					"cursor": nil,
				},
			},
		})
	})

	repos, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	if repos[0].FullName != "alice/alpha" || repos[0].Private || repos[0].URL != "https://git.sr.ht/~alice/alpha" {
		t.Errorf("repo[0] = %+v, want alice/alpha public", repos[0])
	}
	if repos[1].FullName != "alice/vault" || !repos[1].Private {
		t.Errorf("repo[1] = %+v, want alice/vault private", repos[1])
	}
}

func TestListReposPagination(t *testing.T) {
	pages := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		wantPath(t, r, "/git")
		q := gqlQuery(t, r)
		if !strings.Contains(q, "repositories") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		pages++
		cursor := any("next")
		if pages > 1 {
			cursor = nil
		}
		writeGQL(w, map[string]any{
			"me": map[string]any{
				"username": "alice",
				"repositories": map[string]any{
					"results": []map[string]any{{"name": "repo" + strings.Repeat("x", pages), "visibility": "PUBLIC"}},
					"cursor":  cursor,
				},
			},
		})
	})

	repos, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if pages != 2 {
		t.Errorf("expected 2 pages, got %d", pages)
	}
	if len(repos) != 2 || repos[0].FullName != "alice/repox" || repos[1].FullName != "alice/repoxx" {
		t.Errorf("repos = %+v, want both paginated pages in order", repos)
	}
}

func TestStuckCursorFails(t *testing.T) {
	// A server that never returns a nil cursor must not loop forever; the
	// client bails out after maxPages requests.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeGQL(w, map[string]any{
			"me": map[string]any{
				"username": "alice",
				"repositories": map[string]any{
					"results": []map[string]any{{"name": "alpha", "visibility": "PUBLIC"}},
					"cursor":  "same-cursor",
				},
			},
		})
	})

	if _, err := c.ListRepos(context.Background()); err == nil {
		t.Fatal("expected error for a stuck pagination cursor")
	} else if !strings.Contains(err.Error(), "cursor") {
		t.Errorf("error = %q, want mention of the stuck cursor", err)
	}
}

func TestListIssues(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		wantPath(t, r, "/todo")
		q := gqlQuery(t, r)
		switch {
		case strings.Contains(q, "trackers"):
			writeGQL(w, map[string]any{
				"me": map[string]any{
					"username": "alice",
					"trackers": map[string]any{
						"results": []map[string]any{{"name": "bugs"}, {"name": "ideas"}},
						"cursor":  nil,
					},
				},
			})
		case strings.Contains(q, "tickets"):
			// The tickets query is per-tracker via a variable; the fake serves
			// the same two tickets for every tracker and the expectations
			// account for both trackers.
			writeGQL(w, map[string]any{
				"me": map[string]any{
					"tracker": map[string]any{
						"tickets": map[string]any{
							"results": []map[string]any{
								{"id": 10, "ref": "2", "subject": "fix the bug", "status": "CONFIRMED"},
								{"id": 11, "ref": "3", "subject": "old resolved", "status": "RESOLVED"},
							},
							"cursor": nil,
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	issues, err := c.ListIssues(context.Background())
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	// Two trackers, two tickets each, one resolved per tracker -> 2 open.
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2 (resolved filtered out per tracker)", len(issues))
	}
	is := issues[0]
	if is.Repo != "bugs" || is.Number != 2 || is.Title != "fix the bug" || is.State != "confirmed" || is.URL != "https://todo.sr.ht/~alice/bugs/2" {
		t.Errorf("issue = %+v, want bugs #2 fix the bug confirmed", is)
	}
	if issues[1].Repo != "ideas" || issues[1].Number != 2 {
		t.Errorf("issue[1] = %+v, want ideas #2", issues[1])
	}
}

func TestListIssuesURLFallback(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		wantPath(t, r, "/todo")
		q := gqlQuery(t, r)
		switch {
		case strings.Contains(q, "trackers"):
			writeGQL(w, map[string]any{
				"me": map[string]any{
					"username": "alice",
					"trackers": map[string]any{"results": []map[string]any{{"name": "bugs"}}, "cursor": nil},
				},
			})
		case strings.Contains(q, "tickets"):
			writeGQL(w, map[string]any{
				"me": map[string]any{
					"tracker": map[string]any{
						"tickets": map[string]any{
							"results": []map[string]any{
								// A canonical ref with a path must be reduced to
								// its last segment for the number and URL.
								{"id": 10, "ref": "~alice/bugs/2", "subject": "canonical ref", "status": "REPORTED"},
								// No ref -> the client must fall back to the id.
								{"id": 11, "subject": "no ref", "status": "REPORTED"},
							},
							"cursor": nil,
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	issues, err := c.ListIssues(context.Background())
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2", len(issues))
	}
	if issues[0].Number != 2 || issues[0].URL != "https://todo.sr.ht/~alice/bugs/2" {
		t.Errorf("issue[0] = %+v, want #2 with last-segment ref", issues[0])
	}
	if issues[1].Number != 11 || issues[1].URL != "https://todo.sr.ht/~alice/bugs/11" {
		t.Errorf("issue[1] = %+v, want #11 with fallback URL", issues[1])
	}
}

func TestListPRs(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		wantPath(t, r, "/lists")
		q := gqlQuery(t, r)
		switch {
		case strings.Contains(q, "lists"):
			writeGQL(w, map[string]any{
				"me": map[string]any{
					"username": "alice",
					"lists":    map[string]any{"results": []map[string]any{{"name": "dev"}}, "cursor": nil},
				},
			})
		case strings.Contains(q, "patches"):
			writeGQL(w, map[string]any{
				"me": map[string]any{
					"list": map[string]any{
						"patches": map[string]any{
							"results": []map[string]any{
								{"id": 42, "subject": "add feature", "status": "PROPOSED"},
								{"id": 43, "subject": "review me", "status": "NEEDS_REVISION"},
								{"id": 44, "subject": "applied patch", "status": "APPLIED"},
								{"id": 45, "subject": "superseded", "status": "SUPERSEDED"},
								{"id": 46, "subject": "unknown legacy", "status": "UNKNOWN"},
							},
							"cursor": nil,
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	prs, err := c.ListPRs(context.Background())
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 3 {
		t.Fatalf("got %d PRs, want 3 (applied/superseded filtered out)", len(prs))
	}
	if prs[0].Title != "add feature" || prs[0].State != "proposed" || prs[0].URL != "https://lists.sr.ht/~alice/dev/patches/42" {
		t.Errorf("prs[0] = %+v, want add feature proposed", prs[0])
	}
	if prs[1].Title != "review me" || prs[1].State != "needs_revision" {
		t.Errorf("prs[1] = %+v, want review me needs_revision", prs[1])
	}
	if prs[2].Title != "unknown legacy" {
		t.Errorf("prs[2] = %+v, want unknown legacy kept open", prs[2])
	}
}

func TestGraphQLError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{{"message": "insufficient scope"}},
		})
	})

	if _, err := c.ListRepos(context.Background()); err == nil {
		t.Fatal("expected error for graphql errors response")
	} else if !strings.Contains(err.Error(), "insufficient scope") {
		t.Errorf("error = %q, want to include graphql message", err)
	}
}

func TestGraphQLErrorOn4xx(t *testing.T) {
	// SourceHut returns 4xx with a GraphQL errors body (e.g. a bad token);
	// the client must surface the message, not the bare status.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{{"message": "Invalid OAuth bearer token"}},
		})
	})

	_, err := c.ListRepos(context.Background())
	if err == nil {
		t.Fatal("expected error for unauthorized graphql response")
	}
	if !strings.Contains(err.Error(), "Invalid OAuth bearer token") {
		t.Errorf("error = %q, want to include the graphql message", err)
	}
	if strings.Contains(err.Error(), "401") {
		t.Errorf("error = %q, want the graphql message instead of the bare status", err)
	}
}

func TestMalformedBodyErrors(t *testing.T) {
	// A 200 with an invalid JSON body must surface an error, not silently
	// produce an empty result list.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("this is not json"))
	})

	if _, err := c.ListRepos(context.Background()); err == nil {
		t.Fatal("expected error for malformed response body")
	}
}

func TestNewRequiresToken(t *testing.T) {
	if _, err := New(operations.SourceHutConfig{}); err == nil {
		t.Fatal("expected error when token is missing")
	}
}

func TestSetInstance(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if c.Name() != "sourcehut" {
		t.Errorf("Name() = %q, want sourcehut by default", c.Name())
	}
	c.SetInstance("srht")
	if c.Name() != "srht" {
		t.Errorf("Name() = %q, want srht", c.Name())
	}
}

func TestCreateRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		wantPath(t, r, "/git")
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding graphql request: %v", err)
		}
		if strings.Contains(req.Query, "me {") {
			writeGQL(w, map[string]any{"me": map[string]any{"username": "~you"}})
			return
		}
		input := req.Variables["input"].(map[string]any)
		if input["name"] != "alpha" {
			t.Errorf("input name = %v, want alpha", input["name"])
		}
		if input["visibility"] != "PRIVATE" {
			t.Errorf("visibility = %v, want PRIVATE", input["visibility"])
		}
		writeGQL(w, map[string]any{
			"createRepository": map[string]any{
				"repository": map[string]any{"name": "alpha", "visibility": "PRIVATE"},
			},
		})
	})

	repo, err := c.CreateRepo(context.Background(), operations.RepoInput{Name: "alpha", Private: true})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repo.FullName != "you/alpha" || !repo.Private {
		t.Errorf("repo = %+v, want you/alpha private", repo)
	}
}

func TestRenameRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		wantPath(t, r, "/git")
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding graphql request: %v", err)
		}
		input := req.Variables["input"].(map[string]any)
		if input["repo"] != "~you/alpha" {
			t.Errorf("input repo = %v, want ~you/alpha", input["repo"])
		}
		if input["name"] != "beta" {
			t.Errorf("input name = %v, want beta", input["name"])
		}
		writeGQL(w, map[string]any{
			"updateRepository": map[string]any{
				"repository": map[string]any{"name": "beta", "visibility": "PUBLIC"},
			},
		})
	})

	repo, err := c.RenameRepo(context.Background(), "you/alpha", "beta")
	if err != nil {
		t.Fatalf("RenameRepo: %v", err)
	}
	if repo.FullName != "you/beta" {
		t.Errorf("repo = %+v, want you/beta", repo)
	}
}

func TestDeleteRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkAuth(t, r)
		wantPath(t, r, "/git")
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding graphql request: %v", err)
		}
		input := req.Variables["input"].(map[string]any)
		if input["repo"] != "~you/alpha" {
			t.Errorf("input repo = %v, want ~you/alpha", input["repo"])
		}
		writeGQL(w, map[string]any{"deleteRepository": map[string]any{"id": "1"}})
	})

	if err := c.DeleteRepo(context.Background(), "you/alpha"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
}

func TestRunsWorkflowsProjectsNotSupported(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := c.ListRuns(context.Background(), "you/alpha"); err == nil {
		t.Error("expected error for ListRuns")
	}
	if _, err := c.ListWorkflows(context.Background(), "you/alpha"); err == nil {
		t.Error("expected error for ListWorkflows")
	}
	if _, err := c.ListProjects(context.Background()); err == nil {
		t.Error("expected error for ListProjects")
	}
}

func TestSplitHutName(t *testing.T) {
	tests := []struct {
		full        string
		owner, name string
		ok          bool
	}{
		{"you/alpha", "you", "alpha", true},
		{"~you/alpha", "you", "alpha", true},
		{"group/sub/repo", "group", "sub/repo", true},
		{"no-slash", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		owner, name, ok := splitHutName(tt.full)
		if owner != tt.owner || name != tt.name || ok != tt.ok {
			t.Errorf("splitHutName(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.full, owner, name, ok, tt.owner, tt.name, tt.ok)
		}
	}
}

func TestOpenTicketStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"reported", "REPORTED", true},
		{"confirmed", "CONFIRMED", true},
		{"in progress", "IN_PROGRESS", true},
		{"pending", "PENDING", true},
		{"resolved is closed", "RESOLVED", false},
		{"unknown status", "banana", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openTicketStatus(tt.status); got != tt.want {
				t.Errorf("openTicketStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestOpenPatchsetStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"proposed", "PROPOSED", true},
		{"needs revision", "NEEDS_REVISION", true},
		{"unknown kept open", "UNKNOWN", true},
		{"applied", "APPLIED", false},
		{"superseded", "SUPERSEDED", false},
		{"rejected", "REJECTED", false},
		{"approved is terminal", "APPROVED", false},
		{"unknown status", "banana", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openPatchsetStatus(tt.status); got != tt.want {
				t.Errorf("openPatchsetStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
