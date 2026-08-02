package forgejo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pratyay360/forge/v1/operations"
)

// newTestClient starts a fake Forgejo API and returns a client pointed at it.
// The SDK appends /api/v1 to the configured base and probes /version during
// NewClient, so every handler should serve it (done here via a wrapper).
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			json.NewEncoder(w).Encode(map[string]any{"version": "1.22.0"})
			return
		}
		handler(w, r)
	}))
	t.Cleanup(ts.Close)

	c, err := New(operations.ForgejoConfig{Token: "secret", URL: ts.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestListRepos(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/repos" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "full_name": "alice/alpha", "html_url": "https://codeberg.org/alice/alpha", "private": false},
			{"id": 2, "full_name": "alice/vault", "html_url": "https://codeberg.org/alice/vault", "private": true},
		})
	})

	repos, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	if repos[0].FullName != "alice/alpha" || repos[0].Private || repos[0].URL != "https://codeberg.org/alice/alpha" {
		t.Errorf("repo[0] = %+v, want alice/alpha public", repos[0])
	}
	if repos[1].FullName != "alice/vault" || !repos[1].Private {
		t.Errorf("repo[1] = %+v, want alice/vault private", repos[1])
	}
}

func TestListIssuesAssignedOnly(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/user":
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "login": "alice"})
		case "/api/v1/repos/issues/search":
			// The search endpoint returns pull requests too unless type=issues
			// is sent; the client must request issues only.
			if got := r.URL.Query().Get("type"); got != "issues" {
				t.Errorf("type param = %q, want %q", got, "issues")
			}
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Errorf("state param = %q, want %q", got, "open")
			}
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"id": 1, "number": 10, "title": "mine", "state": "open",
					"html_url":   "https://codeberg.org/alice/alpha/issues/10",
					"repository": map[string]any{"full_name": "alice/alpha"},
					"assignees":  []map[string]any{{"login": "alice"}},
				},
				{
					"id": 2, "number": 11, "title": "unassigned", "state": "open",
					"html_url":   "https://codeberg.org/alice/alpha/issues/11",
					"repository": map[string]any{"full_name": "alice/alpha"},
					"assignees":  []map[string]any{},
				},
				{
					"id": 3, "number": 12, "title": "someone elses", "state": "open",
					"html_url":   "https://codeberg.org/alice/alpha/issues/12",
					"repository": map[string]any{"full_name": "alice/alpha"},
					"assignees":  []map[string]any{{"login": "bob"}},
				},
				// state filtering is handled server-side by the API (the client
				// requests state=open), so the fake only returns open issues.
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	issues, err := c.ListIssues(context.Background())
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (only open issues assigned to alice)", len(issues))
	}
	is := issues[0]
	if is.Repo != "alice/alpha" || is.Number != 10 || is.Title != "mine" || is.State != "open" {
		t.Errorf("issue = %+v, want alice/alpha #10 mine open", is)
	}
}

func TestSetInstance(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if c.Name() != "forgejo" {
		t.Errorf("Name() = %q, want forgejo by default", c.Name())
	}
	c.SetInstance("codeberg")
	if c.Name() != "codeberg" {
		t.Errorf("Name() = %q, want codeberg", c.Name())
	}
}

func TestCreateRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/repos" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "alpha" || body["private"] != true {
			t.Errorf("body = %v, want name=alpha private=true", body)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": 1, "full_name": "alice/alpha", "html_url": "https://codeberg.org/alice/alpha", "private": true,
		})
	})

	repo, err := c.CreateRepo(context.Background(), operations.RepoInput{Name: "alpha", Private: true})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repo.FullName != "alice/alpha" || !repo.Private {
		t.Errorf("repo = %+v, want alice/alpha private", repo)
	}
}

func TestCreateOrgRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/org/acme/repos" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": 2, "full_name": "acme/alpha", "html_url": "https://codeberg.org/acme/alpha", "private": false,
		})
	})

	repo, err := c.CreateRepo(context.Background(), operations.RepoInput{Name: "alpha", Owner: "acme"})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repo.FullName != "acme/alpha" {
		t.Errorf("repo = %+v, want acme/alpha", repo)
	}
}

func TestRenameRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/alice/alpha" || r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "beta" {
			t.Errorf("body = %v, want name=beta", body)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": 1, "full_name": "alice/beta", "html_url": "https://codeberg.org/alice/beta", "private": false,
		})
	})

	repo, err := c.RenameRepo(context.Background(), "alice/alpha", "beta")
	if err != nil {
		t.Fatalf("RenameRepo: %v", err)
	}
	if repo.FullName != "alice/beta" {
		t.Errorf("repo = %+v, want alice/beta", repo)
	}
}

func TestDeleteRepo(t *testing.T) {
	var method, path string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteRepo(context.Background(), "alice/alpha"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	if method != http.MethodDelete || path != "/api/v1/repos/alice/alpha" {
		t.Errorf("got %s %s, want DELETE /api/v1/repos/alice/alpha", method, path)
	}
}

func TestSetVisibility(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/alice/alpha" || r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["private"] != true {
			t.Errorf("body = %v, want private=true", body)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": 1, "full_name": "alice/alpha", "html_url": "https://codeberg.org/alice/alpha", "private": true,
		})
	})

	repo, err := c.SetVisibility(context.Background(), "alice/alpha", true)
	if err != nil {
		t.Fatalf("SetVisibility: %v", err)
	}
	if !repo.Private {
		t.Errorf("repo = %+v, want private", repo)
	}
}

func TestRunsWorkflowsProjectsNotSupported(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := c.ListRuns(context.Background(), "alice/alpha"); err == nil {
		t.Error("expected error for ListRuns")
	}
	if _, err := c.ListWorkflows(context.Background(), "alice/alpha"); err == nil {
		t.Error("expected error for ListWorkflows")
	}
	if _, err := c.ListProjects(context.Background()); err == nil {
		t.Error("expected error for ListProjects")
	}
}

func TestSplitFullName(t *testing.T) {
	tests := []struct {
		full        string
		owner, name string
		ok          bool
	}{
		{"alice/alpha", "alice", "alpha", true},
		{"group/sub/repo", "group", "sub/repo", true},
		{"no-slash", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		owner, name, ok := splitFullName(tt.full)
		if owner != tt.owner || name != tt.name || ok != tt.ok {
			t.Errorf("splitFullName(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.full, owner, name, ok, tt.owner, tt.name, tt.ok)
		}
	}
}

func TestListPRs(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/user/repos":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "full_name": "alice/alpha", "html_url": "https://codeberg.org/alice/alpha", "private": false},
				{"id": 2, "full_name": "alice/empty", "html_url": "https://codeberg.org/alice/empty", "private": false},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/repos/alice/"):
			if !strings.HasSuffix(r.URL.Path, "/pulls") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Errorf("state param = %q, want %q", got, "open")
			}
			if strings.Contains(r.URL.Path, "empty") {
				json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "number": 7, "title": "add feature", "state": "open", "html_url": "https://codeberg.org/alice/alpha/pulls/7"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	prs, err := c.ListPRs(context.Background())
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	pr := prs[0]
	if pr.Repo != "alice/alpha" || pr.Number != 7 || pr.Title != "add feature" || pr.URL != "https://codeberg.org/alice/alpha/pulls/7" {
		t.Errorf("pr = %+v, want alice/alpha #7 add feature", pr)
	}
}
