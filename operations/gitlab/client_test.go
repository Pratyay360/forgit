package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pratyay360/forge/v1/operations"
)

// newTestClient starts a fake GitLab API and returns a client pointed at it.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	c, err := New(operations.GitLabConfig{Token: "secret", URL: ts.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestListRepos(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/projects") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"path_with_namespace": "alice/alpha", "web_url": "https://gitlab.com/alice/alpha", "visibility": "public"},
			{"path_with_namespace": "alice/vault", "web_url": "https://gitlab.com/alice/vault", "visibility": "private"},
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
		repo    operations.Repo
		priv    bool
		wantURL string
	}{
		{repos[0], false, "https://gitlab.com/alice/alpha"},
		{repos[1], true, "https://gitlab.com/alice/vault"},
	}
	for _, tt := range tests {
		if tt.repo.Private != tt.priv || tt.repo.URL != tt.wantURL {
			t.Errorf("repo = %+v, want private=%v url=%s", tt.repo, tt.priv, tt.wantURL)
		}
	}
}

func TestListIssues(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/issues") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("scope") != "assigned_to_me" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": 1, "iid": 42, "title": "fix the bug", "state": "opened", "project_id": 7,
				"web_url": "https://gitlab.com/group/project/-/issues/42",
			},
		})
	})

	issues, err := c.ListIssues(context.Background())
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	is := issues[0]
	if is.Repo != "group/project" || is.Number != 42 || is.Title != "fix the bug" || is.State != "opened" {
		t.Errorf("issue = %+v, want repo=group/project #42 fix the bug", is)
	}
}

func TestListPRs(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/merge_requests") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": 2, "iid": 3, "title": "add feature", "state": "opened", "project_id": 7,
				"web_url": "https://gitlab.com/group/sub/project/-/merge_requests/3",
			},
		})
	})

	prs, err := c.ListPRs(context.Background())
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	pr := prs[0]
	if pr.Repo != "group/sub/project" || pr.Number != 3 || pr.Title != "add feature" {
		t.Errorf("pr = %+v, want repo=group/sub/project #3 add feature", pr)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"bare host gets api/v4", "https://gitlab.example.com", "https://gitlab.example.com/api/v4"},
		{"trailing slash stripped", "https://gitlab.example.com/", "https://gitlab.example.com/api/v4"},
		{"full api path kept", "https://gitlab.example.com/api/v4", "https://gitlab.example.com/api/v4"},
		{"api path with trailing slash", "https://gitlab.example.com/api/v4/", "https://gitlab.example.com/api/v4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBaseURL(tt.raw); got != tt.want {
				t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSetInstance(t *testing.T) {
	c, err := New(operations.GitLabConfig{Token: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Name() != "gitlab" {
		t.Errorf("Name() = %q, want gitlab by default", c.Name())
	}
	c.SetInstance("gitlab-work")
	if c.Name() != "gitlab-work" {
		t.Errorf("Name() = %q, want gitlab-work", c.Name())
	}
}

func TestCreateRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "alpha" || body["path"] != "alpha" {
			t.Errorf("body = %v, want name=alpha path=alpha", body)
		}
		if body["visibility"] != "private" {
			t.Errorf("visibility = %v, want private", body["visibility"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"path_with_namespace": "alice/alpha", "web_url": "https://gitlab.com/alice/alpha", "visibility": "private",
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

func TestCreateRepoWithOwner(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := c.CreateRepo(context.Background(), operations.RepoInput{Name: "alpha", Owner: "acme"})
	if err == nil {
		t.Fatal("expected error for group-owned creation")
	}
}

func TestRenameRepo(t *testing.T) {
	var path string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		path = r.URL.Path
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "beta" || body["path"] != "beta" {
			t.Errorf("body = %v, want name=beta path=beta", body)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"path_with_namespace": "alice/beta", "web_url": "https://gitlab.com/alice/beta", "visibility": "public",
		})
	})

	repo, err := c.RenameRepo(context.Background(), "alice/alpha", "beta")
	if err != nil {
		t.Fatalf("RenameRepo: %v", err)
	}
	if path != "/api/v4/projects/alice/alpha" {
		t.Errorf("path = %q, want /api/v4/projects/alice/alpha", path)
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
	if method != http.MethodDelete || path != "/api/v4/projects/alice/alpha" {
		t.Errorf("got %s %s, want DELETE /api/v4/projects/alice/alpha", method, path)
	}
}

func TestSetVisibility(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v4/projects/alice/alpha" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["visibility"] != "private" {
			t.Errorf("visibility = %v, want private", body["visibility"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"path_with_namespace": "alice/alpha", "web_url": "https://gitlab.com/alice/alpha", "visibility": "private",
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

func TestListRuns(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/alice/alpha/pipelines" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 100, "name": "pipeline", "status": "success", "ref": "main", "web_url": "https://gitlab.com/alice/alpha/-/pipelines/100"},
			{"id": 101, "name": "pipeline", "status": "running", "ref": "dev", "web_url": "https://gitlab.com/alice/alpha/-/pipelines/101"},
		})
	})

	runs, err := c.ListRuns(context.Background(), "alice/alpha")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}
	if runs[0].ID != 100 || runs[0].Status != "success" || runs[0].Branch != "main" || runs[0].Repo != "alice/alpha" {
		t.Errorf("runs[0] = %+v, want pipeline 100 success on main", runs[0])
	}
}

func TestListWorkflowsNotSupported(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := c.ListWorkflows(context.Background(), "alice/alpha"); err == nil {
		t.Fatal("expected ErrNotSupported for gitlab workflows")
	}
}

func TestListProjects(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("owned") != "true" {
			t.Errorf("owned = %q, want true", r.URL.Query().Get("owned"))
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"path_with_namespace": "alice/alpha", "web_url": "https://gitlab.com/alice/alpha", "visibility": "private"},
		})
	})

	projects, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].FullName != "alice/alpha" || !projects[0].Private {
		t.Errorf("projects = %+v, want alice/alpha private", projects)
	}
}

func TestRepoFromWebURL(t *testing.T) {
	tests := []struct {
		name      string
		webURL    string
		projectID int
		want      string
	}{
		{"issue url", "https://gitlab.com/group/project/-/issues/42", 7, "group/project"},
		{"nested subgroup merge request", "https://gitlab.com/group/sub/project/-/merge_requests/3", 8, "group/sub/project"},
		{"plain project url", "https://gitlab.com/group/project", 9, "group/project"},
		{"self-hosted instance", "https://gitlab.example.com/team/repo/-/issues/1", 10, "team/repo"},
		{"empty url falls back to project id", "", 5, "project/5"},
		{"malformed url falls back to project id", "://not-a-url", 6, "project/6"},
		// Regression: a project literally named "issues" or "merge_requests"
		// must not be mistaken for the /-/ marker.
		{"project named issues", "https://gitlab.com/group/issues/-/issues/1", 11, "group/issues"},
		{"project named merge_requests", "https://gitlab.com/team/merge_requests/-/merge_requests/2", 12, "team/merge_requests"},
		{"global issue url has no project", "https://gitlab.com/-/issues/1", 13, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := repoFromWebURL(tt.webURL, tt.projectID); got != tt.want {
				t.Errorf("repoFromWebURL(%q, %d) = %q, want %q", tt.webURL, tt.projectID, got, tt.want)
			}
		})
	}
}
