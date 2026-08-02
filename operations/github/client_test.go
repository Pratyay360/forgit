package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	gh "github.com/google/go-github/v89/github"
	"github.com/pratyay360/forge/v1/operations"
)

// newTestClient starts a fake GitHub API and returns a client pointed at it.
//
// The production New() has no URL knob, so the inner go-github client is built
// directly with WithURLs; the trailing slash is required by NewRequest. The
// token is injected via WithAuthToken so the fake can assert auth plumbing.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	base := ts.URL + "/"
	inner, err := gh.NewClient(gh.WithURLs(&base, &base), gh.WithAuthToken("secret"))
	if err != nil {
		t.Fatalf("building go-github client: %v", err)
	}
	return &Client{inner: inner}
}

func TestNew(t *testing.T) {
	c, err := New(operations.GitHubConfig{Token: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Name() != "github" {
		t.Errorf("Name() = %q, want github", c.Name())
	}
}

func TestNewRequiresToken(t *testing.T) {
	if _, err := New(operations.GitHubConfig{}); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestListRepos(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("per_page = %q, want 100", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"full_name": "alice/alpha", "html_url": "https://github.com/alice/alpha", "private": false},
			{"full_name": "alice/vault", "html_url": "https://github.com/alice/vault", "private": true},
		})
	})

	repos, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	for i, want := range []struct {
		fullName string
		private  bool
		url      string
	}{
		{"alice/alpha", false, "https://github.com/alice/alpha"},
		{"alice/vault", true, "https://github.com/alice/vault"},
	} {
		got := repos[i]
		if got.Forge != "github" || got.FullName != want.fullName || got.Private != want.private || got.URL != want.url {
			t.Errorf("repo[%d] = %+v, want forge=github full_name=%s private=%v url=%s", i, got, want.fullName, want.private, want.url)
		}
	}
}

func TestListReposError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.ListRepos(context.Background()); err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestListIssues(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/issues" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// The client must ask for open issues assigned to the authenticated
		// user, mirroring GitHub's filter=assigned/state=open contract.
		if got := r.URL.Query().Get("filter"); got != "assigned" {
			t.Errorf("filter = %q, want assigned", got)
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state = %q, want open", got)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 42, "title": "fix the bug", "state": "open",
				"html_url":   "https://github.com/alice/alpha/issues/42",
				"repository": map[string]any{"full_name": "alice/alpha"},
			},
			{
				// Pull requests surface in the issues API and must be filtered
				// out client-side.
				"number": 7, "title": "a PR in disguise", "state": "open",
				"html_url":     "https://github.com/alice/alpha/pull/7",
				"pull_request": map[string]any{"url": "https://api.github.com/repos/alice/alpha/pulls/7"},
			},
		})
	})

	issues, err := c.ListIssues(context.Background())
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (pull request filtered out)", len(issues))
	}
	is := issues[0]
	if is.Forge != "github" || is.Repo != "alice/alpha" || is.Number != 42 || is.Title != "fix the bug" || is.State != "open" || is.URL != "https://github.com/alice/alpha/issues/42" {
		t.Errorf("issue = %+v, want alice/alpha #42 fix the bug open", is)
	}
}

func TestListIssuesRepoFallback(t *testing.T) {
	// Some GitHub responses omit the nested repository object; the repo name
	// must then be derived from repository_url.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 5, "title": "no repo object", "state": "open",
				"html_url":       "https://github.com/org/proj/issues/5",
				"repository_url": "https://api.github.com/repos/org/proj",
			},
		})
	})

	issues, err := c.ListIssues(context.Background())
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].Repo != "org/proj" {
		t.Errorf("got %+v, want repo org/proj derived from repository_url", issues)
	}
}

func TestListIssuesError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.ListIssues(context.Background()); err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestListPRs(t *testing.T) {
	var (
		mu      sync.Mutex
		scanned int
	)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user/repos":
			// First entry has no owner/name and must be skipped client-side
			// without any pulls request.
			repos := []map[string]any{{"full_name": "alice/broken"}}
			for i := 1; i <= 12; i++ {
				repos = append(repos, map[string]any{
					"full_name": fmt.Sprintf("alice/repo%02d", i),
					"name":      fmt.Sprintf("repo%02d", i),
					"owner":     map[string]any{"login": "alice"},
				})
			}
			json.NewEncoder(w).Encode(repos)
		case strings.HasPrefix(r.URL.Path, "/repos/alice/repo") && strings.HasSuffix(r.URL.Path, "/pulls"):
			mu.Lock()
			scanned++
			mu.Unlock()
			repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/alice/"), "/pulls")
			if repo > "repo10" {
				t.Errorf("scanned repo beyond the 10-repo cap: %s", repo)
			}
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Errorf("state = %q, want open", got)
			}
			if got := r.URL.Query().Get("per_page"); got != "50" {
				t.Errorf("per_page = %q, want 50", got)
			}
			switch repo {
			case "repo02": // repository with no pull requests
				json.NewEncoder(w).Encode([]map[string]any{})
			case "repo03": // API failure -> repo skipped, not fatal
				w.WriteHeader(http.StatusInternalServerError)
			default:
				json.NewEncoder(w).Encode([]map[string]any{
					{"number": 1, "title": "PR in " + repo, "state": "open", "html_url": "https://github.com/alice/" + repo + "/pull/1"},
				})
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	// Capture the stderr warning emitted for the failing repository.
	oldStderr := os.Stderr
	rp, wp, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wp
	defer func() { os.Stderr = oldStderr }()

	prs, err := c.ListPRs(context.Background())
	wp.Close()
	out, _ := io.ReadAll(rp)

	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 7 {
		t.Fatalf("got %d PRs, want 7 (9 scanned repos minus 1 empty and 1 failed)", len(prs))
	}
	mu.Lock()
	gotScanned := scanned
	mu.Unlock()
	// 13 repos in the fixture: the owner-less one is skipped client-side, so
	// exactly 9 pulls requests are made (repos 1-9; the cap leaves 3 unscanned).
	if gotScanned != 9 {
		t.Errorf("scanned %d repositories, want 9 (13 repos, cap is 10, 1 skipped for missing owner/name)", gotScanned)
	}
	if !strings.Contains(string(out), "skipped 1 repository") {
		t.Errorf("stderr = %q, want a warning about the skipped repository", string(out))
	}
	pr := prs[0]
	if pr.Forge != "github" || pr.Repo != "alice/repo01" || pr.Number != 1 || pr.Title != "PR in repo01" || pr.State != "open" {
		t.Errorf("prs[0] = %+v, want alice/repo01 #1 PR in repo01", pr)
	}
}

func TestListPRsReposError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.ListPRs(context.Background()); err == nil {
		t.Fatal("expected error when listing repositories fails")
	}
}

func TestRepoFullName(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		name string
		is   gh.Issue
		want string
	}{
		{"repository full name", gh.Issue{Repository: &gh.Repository{FullName: str("a/b")}}, "a/b"},
		{"empty full name falls back to url", gh.Issue{Repository: &gh.Repository{}, RepositoryURL: str("https://api.github.com/repos/x/y")}, "x/y"},
		{"no repository falls back to url", gh.Issue{RepositoryURL: str("https://api.github.com/repos/x/y")}, "x/y"},
		{"non-api url", gh.Issue{RepositoryURL: str("https://example.com/foo")}, ""},
		{"nothing set", gh.Issue{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := repoFullName(&tt.is); got != tt.want {
				t.Errorf("repoFullName(%+v) = %q, want %q", tt.is, got, tt.want)
			}
		})
	}
}

func TestSetInstance(t *testing.T) {
	c, err := New(operations.GitHubConfig{Token: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Name() != "github" {
		t.Errorf("Name() = %q, want github by default", c.Name())
	}
	c.SetInstance("github-main")
	if c.Name() != "github-main" {
		t.Errorf("Name() = %q, want github-main", c.Name())
	}
}

func TestCreateRepo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "alpha" {
			t.Errorf("name = %v, want alpha", body["name"])
		}
		if body["private"] != true {
			t.Errorf("private = %v, want true", body["private"])
		}
		if body["description"] != "new repo" {
			t.Errorf("description = %v, want new repo", body["description"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"full_name": "alice/alpha", "html_url": "https://github.com/alice/alpha", "private": true,
		})
	})

	repo, err := c.CreateRepo(context.Background(), operations.RepoInput{
		Name: "alpha", Description: "new repo", Private: true,
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repo.FullName != "alice/alpha" || !repo.Private || repo.Forge != "github" {
		t.Errorf("repo = %+v, want alice/alpha private", repo)
	}
}

func TestCreateRepoOrg(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/acme/repos" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"full_name": "acme/alpha", "html_url": "https://github.com/acme/alpha", "private": false,
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
		if r.URL.Path != "/repos/alice/alpha" || r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "beta" {
			t.Errorf("name = %v, want beta", body["name"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"full_name": "alice/beta", "html_url": "https://github.com/alice/beta", "private": false,
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
	var path string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/alice/alpha" || r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteRepo(context.Background(), "alice/alpha"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	if path != "/repos/alice/alpha" {
		t.Errorf("deleted %q, want /repos/alice/alpha", path)
	}
}

func TestSetVisibility(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/alice/alpha" || r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["private"] != true {
			t.Errorf("private = %v, want true", body["private"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"full_name": "alice/alpha", "html_url": "https://github.com/alice/alpha", "private": true,
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
		if r.URL.Path != "/repos/alice/alpha/actions/runs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"workflow_runs": []map[string]any{
				{"id": 10, "name": "ci", "status": "completed", "conclusion": "success", "head_branch": "main", "html_url": "https://github.com/alice/alpha/actions/runs/10"},
				{"id": 11, "name": "ci", "status": "in_progress", "conclusion": "", "head_branch": "dev", "html_url": "https://github.com/alice/alpha/actions/runs/11"},
			},
		})
	})

	runs, err := c.ListRuns(context.Background(), "alice/alpha")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}
	if runs[0].ID != 10 || runs[0].Status != "success" || runs[0].Branch != "main" {
		t.Errorf("runs[0] = %+v, want run 10 success on main", runs[0])
	}
	if runs[1].Status != "in_progress" {
		t.Errorf("runs[1] status = %q, want in_progress", runs[1].Status)
	}
}

func TestListWorkflows(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/alice/alpha/actions/workflows" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"total_count": 1,
			"workflows": []map[string]any{
				{"id": 1, "name": "ci.yml", "state": "active", "html_url": "https://github.com/alice/alpha/actions/workflows/ci.yml"},
			},
		})
	})

	workflows, err := c.ListWorkflows(context.Background(), "alice/alpha")
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(workflows) != 1 {
		t.Fatalf("got %d workflows, want 1", len(workflows))
	}
	wf := workflows[0]
	if wf.ID != 1 || wf.Name != "ci.yml" || wf.State != "active" || wf.Repo != "alice/alpha" {
		t.Errorf("workflow = %+v, want ci.yml active on alice/alpha", wf)
	}
}

func TestListProjects(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			json.NewEncoder(w).Encode(map[string]any{"login": "alice"})
		case r.URL.Path == "/users/alice/projectsV2":
			json.NewEncoder(w).Encode([]map[string]any{
				{"name": "Roadmap", "html_url": "https://github.com/users/alice/projects/1", "public": false},
				{"name": "Ideas", "html_url": "https://github.com/users/alice/projects/2", "public": true},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	projects, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
	if projects[0].FullName != "Roadmap" || !projects[0].Private {
		t.Errorf("projects[0] = %+v, want Roadmap private", projects[0])
	}
	if projects[1].Private {
		t.Errorf("projects[1] = %+v, want public", projects[1])
	}
}

func TestSplitFullName(t *testing.T) {
	tests := []struct {
		full        string
		owner, name string
	}{
		{"alice/alpha", "alice", "alpha"},
		{"group/sub/repo", "group", "sub/repo"},
		{"bare", "", "bare"},
	}
	for _, tt := range tests {
		owner, name := splitFullName(tt.full)
		if owner != tt.owner || name != tt.name {
			t.Errorf("splitFullName(%q) = (%q, %q), want (%q, %q)", tt.full, owner, name, tt.owner, tt.name)
		}
	}
}
