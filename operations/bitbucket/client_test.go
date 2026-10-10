package bitbucket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pratyay360/forgit/operations"
)

func TestApiErrorHints(t *testing.T) {
	// Scopes error hint
	err := apiError(http.MethodGet, "https://api.bitbucket.org/2.0/repositories", "401 Unauthorized", []byte(`{"type":"error","error":{"message":"API Token provided has no Bitbucket scopes."}}`))
	if !strings.Contains(err.Error(), "your Atlassian API token needs Bitbucket scopes") {
		t.Errorf("expected scopes hint in error, got: %v", err)
	}

	// Basic auth email hint
	err = apiError(http.MethodGet, "https://api.bitbucket.org/2.0/user", "401 Unauthorized", []byte(`{"type":"error","error":{"message":"API token must be used with an atlassian registered email"}}`))
	if !strings.Contains(err.Error(), "API tokens/app passwords need Basic auth") {
		t.Errorf("expected basic auth hint in error, got: %v", err)
	}
}

func TestListReposGlobal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repositories" {
			if r.URL.Query().Get("role") != "member" {
				t.Errorf("expected query role=member, got: %s", r.URL.RawQuery)
			}
			resp := map[string]any{
				"values": []map[string]any{
					{
						"full_name":  "myws/repo1",
						"is_private": false,
						"links": map[string]any{
							"html": map[string]any{"href": "https://bitbucket.org/myws/repo1"},
						},
						"workspace": map[string]any{"slug": "myws"},
					},
					{
						"full_name":  "myws/repo2",
						"is_private": true,
						"links": map[string]any{
							"html": map[string]any{"href": "https://bitbucket.org/myws/repo2"},
						},
						"workspace": map[string]any{"slug": "myws"},
					},
				},
				"next": "",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	client, err := New(operations.BitbucketConfig{Token: "dummy", Username: "user@example.com"})
	if err != nil {
		t.Fatalf("unexpected New err: %v", err)
	}

	repos, err := client.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("unexpected ListRepos err: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0].FullName != "myws/repo1" || repos[0].Private {
		t.Errorf("unexpected repo 0: %+v", repos[0])
	}
	if repos[1].FullName != "myws/repo2" || !repos[1].Private {
		t.Errorf("unexpected repo 1: %+v", repos[1])
	}
}

func TestWorkspacesNeverUsesEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/permissions/workspaces" {
			// Workspace permissions call fails (e.g. 403 or 401)
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Forbidden"}})
			return
		}
		if r.URL.Path == "/user" {
			// /user returns authenticated user username
			_ = json.NewEncoder(w).Encode(map[string]any{"username": "discoveredws"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	client, err := New(operations.BitbucketConfig{Token: "dummy", Username: "user@example.com"})
	if err != nil {
		t.Fatalf("unexpected New err: %v", err)
	}

	ws, err := client.workspaces(context.Background())
	if err != nil {
		t.Fatalf("unexpected workspaces err: %v", err)
	}
	if len(ws) != 1 || ws[0] != "discoveredws" {
		t.Errorf("expected ['discoveredws'], got: %v", ws)
	}
	// Verify email address was not used
	for _, w := range ws {
		if strings.Contains(w, "@") {
			t.Errorf("workspace slug contains '@': %s", w)
		}
	}
}

func TestGetPRAndDiff(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repositories/myws/repo1/pullrequests/42":
			resp := map[string]any{
				"id":    42,
				"title": "Fix bug",
				"state": "OPEN",
				"summary": map[string]any{
					"raw": "This fixes a bug",
				},
				"author": map[string]any{
					"nickname": "octocat",
				},
				"source": map[string]any{
					"branch": map[string]any{"name": "feature"},
					"commit": map[string]any{"hash": "1234567890abcdef"},
					"repository": map[string]any{
						"full_name": "myws/repo1",
					},
				},
				"destination": map[string]any{
					"branch": map[string]any{"name": "main"},
				},
				"links": map[string]any{
					"html": map[string]any{"href": "https://bitbucket.org/myws/repo1/pull-requests/42"},
					"diff": map[string]any{"href": srv.URL + "/diff/42"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/diff/42":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/file b/file\n+newline\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	client, err := New(operations.BitbucketConfig{Token: "dummy", Username: "user@example.com"})
	if err != nil {
		t.Fatalf("unexpected New err: %v", err)
	}

	pr, err := client.GetPR(context.Background(), "myws/repo1", 42)
	if err != nil {
		t.Fatalf("unexpected GetPR err: %v", err)
	}
	if pr.Number != 42 || pr.Title != "Fix bug" || pr.HeadBranch != "feature" || pr.BaseBranch != "main" {
		t.Errorf("unexpected pr detail: %+v", pr)
	}

	diff, err := client.PRDiff(context.Background(), "myws/repo1", 42)
	if err != nil {
		t.Fatalf("unexpected PRDiff err: %v", err)
	}
	if !strings.Contains(diff, "+newline") {
		t.Errorf("expected diff content, got: %q", diff)
	}
}
