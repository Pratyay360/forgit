package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pratyay360/forge/v1/operations"
)

// newTestClient starts a fake Bitbucket API and returns a client pointed at it.
//
// Note: apiBase is a package-level test seam; these tests must not run in
// parallel with each other or with other tests that touch apiBase.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	old := apiBase
	apiBase = ts.URL
	t.Cleanup(func() { apiBase = old })

	c, err := New(operations.BitbucketConfig{Token: "secret", Username: "alice"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestListRepos(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "alice" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{
					"full_name":  "alice/alpha",
					"is_private": false,
					"links":      map[string]any{"html": map[string]any{"href": "https://bitbucket.org/alice/alpha"}},
				},
				{
					"full_name":  "alice/vault",
					"is_private": true,
					"links":      map[string]any{"html": map[string]any{"href": "https://bitbucket.org/alice/vault"}},
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

	tests := []struct {
		name     string
		fullName string
		private  bool
		url      string
	}{
		{"public repo", "alice/alpha", false, "https://bitbucket.org/alice/alpha"},
		{"private repo", "alice/vault", true, "https://bitbucket.org/alice/vault"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repos[0]
			if tt.fullName == "alice/vault" {
				got = repos[1]
			}
			if got.FullName != tt.fullName || got.Private != tt.private || got.URL != tt.url {
				t.Errorf("got %+v, want full_name=%s private=%v url=%s", got, tt.fullName, tt.private, tt.url)
			}
		})
	}
}

func TestListReposUnauthorized(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	if _, err := c.ListRepos(context.Background()); err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}

func TestListIssuesUnsupported(t *testing.T) {
	// Bitbucket retired its issue tracker in 2023; ListIssues must report that
	// via the shared sentinel instead of querying the deprecated API. No HTTP
	// request should be made at all.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("ListIssues must not issue HTTP requests against the retired issue API")
	})

	_, err := c.ListIssues(context.Background())
	if err == nil {
		t.Fatal("expected error for unsupported issue tracking")
	}
	if !errors.Is(err, operations.ErrIssuesUnsupported) {
		t.Errorf("err = %v, want it to wrap operations.ErrIssuesUnsupported", err)
	}
}

func TestListPRs(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repositories/alice"):
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{"full_name": "alice/alpha"}},
			})
		case strings.Contains(r.URL.Path, "/pullrequests"):
			// The real API honors the state=OPEN query param; mimic it by only
			// returning pull requests in the OPEN state.
			if r.URL.Query().Get("state") != "OPEN" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"id": 10, "title": "feature", "state": "OPEN", "links": map[string]any{"html": map[string]any{"href": "https://bitbucket.org/alice/alpha/pull-requests/10"}}},
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
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	if prs[0].Number != 10 || prs[0].Title != "feature" {
		t.Errorf("got %+v, want PR #10 feature", prs[0])
	}
}

func TestSetInstance(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if c.Name() != "bitbucket" {
		t.Errorf("Name() = %q, want bitbucket by default", c.Name())
	}
	c.SetInstance("bb")
	if c.Name() != "bb" {
		t.Errorf("Name() = %q, want bb", c.Name())
	}
}

func TestCreateRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/repositories/alice/alpha" {
			t.Errorf("path = %q, want /repositories/alice/alpha", r.URL.Path)
		}
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["is_private"] != true || payload["scm"] != "git" || payload["description"] != "desc" {
			t.Errorf("payload = %v, want private git with description", payload)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":  "alice/alpha",
			"is_private": true,
			"links":      map[string]any{"html": map[string]any{"href": "https://bitbucket.org/alice/alpha"}},
		})
	})

	repo, err := c.CreateRepo(context.Background(), operations.RepoInput{Name: "alpha", Private: true, Description: "desc"})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repo.FullName != "alice/alpha" || !repo.Private {
		t.Errorf("repo = %+v, want alice/alpha private", repo)
	}
}

func TestRenameRepoUnsupported(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("RenameRepo must not issue HTTP requests")
	})
	_, err := c.RenameRepo(context.Background(), "alice/alpha", "beta")
	if err == nil {
		t.Fatal("expected error for unsupported rename")
	}
	if !errors.Is(err, operations.ErrNotSupported) {
		t.Errorf("err = %v, want it to wrap operations.ErrNotSupported", err)
	}
}

func TestDeleteRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/repositories/alice/alpha" {
			t.Errorf("path = %q, want /repositories/alice/alpha", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteRepo(context.Background(), "alice/alpha"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
}

func TestSetVisibility(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/repositories/alice/vault" {
			t.Errorf("path = %q, want /repositories/alice/vault", r.URL.Path)
		}
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["is_private"] != true {
			t.Errorf("payload = %v, want is_private true", payload)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":  "alice/vault",
			"is_private": true,
			"links":      map[string]any{"html": map[string]any{"href": "https://bitbucket.org/alice/vault"}},
		})
	})

	repo, err := c.SetVisibility(context.Background(), "alice/vault", true)
	if err != nil {
		t.Fatalf("SetVisibility: %v", err)
	}
	if repo.FullName != "alice/vault" || !repo.Private {
		t.Errorf("repo = %+v, want alice/vault private", repo)
	}
}

func TestListRuns(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/repositories/alice/alpha/pipelines/" {
			t.Errorf("path = %q, want /repositories/alice/alpha/pipelines/", r.URL.Path)
		}
		if r.URL.Query().Get("pagelen") != "50" {
			t.Errorf("pagelen = %q, want 50", r.URL.Query().Get("pagelen"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{
					"uuid":   "{abc123}",
					"state":  map[string]any{"name": "COMPLETED", "result": map[string]any{"name": "SUCCESSFUL"}},
					"target": map[string]any{"ref_name": "main"},
					"links":  map[string]any{"html": map[string]any{"href": "https://bitbucket.org/alice/alpha/addon/pipelines/home#!/results/1"}},
				},
			},
		})
	})

	runs, err := c.ListRuns(context.Background(), "alice/alpha")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0].Repo != "alice/alpha" || runs[0].Status != "SUCCESSFUL" || runs[0].Branch != "main" {
		t.Errorf("run = %+v, want alice/alpha SUCCESSFUL on main", runs[0])
	}
}

func TestListWorkflowsUnsupported(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("ListWorkflows must not issue HTTP requests")
	})
	_, err := c.ListWorkflows(context.Background(), "alice/alpha")
	if err == nil {
		t.Fatal("expected error for unsupported workflow definitions")
	}
	if !errors.Is(err, operations.ErrNotSupported) {
		t.Errorf("err = %v, want it to wrap operations.ErrNotSupported", err)
	}
}

func TestListProjects(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/alice/projects" {
			t.Errorf("path = %q, want /workspaces/alice/projects", r.URL.Path)
		}
		if r.URL.Query().Get("pagelen") != "100" {
			t.Errorf("pagelen = %q, want 100", r.URL.Query().Get("pagelen"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"name": "Alpha", "key": "ALP", "links": map[string]any{"html": map[string]any{"href": "https://bitbucket.org/alice/workspace/projects/ALP"}}},
			},
		})
	})

	projects, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
	if projects[0].FullName != "ALP" {
		t.Errorf("project = %+v, want key ALP", projects[0])
	}
}
