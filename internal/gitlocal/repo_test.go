package gitlocal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/pratyay360/forgit/operations"
)

// TestOpenWalksUp confirms that Open finds a parent .git when called from a
// subdirectory, matching the way git itself behaves.
func TestOpenWalksUp(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	sub := filepath.Join(dir, "deep", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	repo, err := Open(sub)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if repo.Root() != dir {
		t.Errorf("Root = %q, want %q", repo.Root(), dir)
	}
}

// TestOpenErrorsCleanly confirms the not-in-a-repo path returns an actionable
// message rather than the raw go-git error.
func TestOpenErrorsCleanly(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !contains(err.Error(), "not inside a git repository") {
		t.Errorf("missing actionable message: %v", err)
	}
}

// TestRemotesEmptyWhenNoRemotes confirms a fresh repository has no remotes.
func TestRemotesEmptyWhenNoRemotes(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	remotes, err := repo.Remotes()
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 0 {
		t.Errorf("len(remotes) = %d, want 0", len(remotes))
	}
}

// TestHeadBranchEmptyOnInit confirms a brand-new repository reports no branch
// until a commit has been made.
func TestHeadBranchEmptyOnInit(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if branch := repo.HeadBranch(); branch != "" {
		t.Errorf("HeadBranch = %q, want \"\"", branch)
	}
}

// TestBindNoMatch reports an actionable error when no remote matches the
// configured instances.
func TestBindNoMatch(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = Bind(repo, operations.Config{}, BindOptions{})
	if err == nil {
		t.Fatal("expected an error when there are no remotes")
	}
	if !contains(err.Error(), "no git remotes") {
		t.Errorf("missing actionable message: %v", err)
	}
}

// TestBindExplicitRepo resolves an explicit --repo value without needing a
// local repository or remotes.
func TestBindExplicitRepo(t *testing.T) {
	cfg := operations.Config{
		GitHub: operations.GitHubConfig{Token: "x"},
	}
	ctx, err := Bind(nil, cfg, BindOptions{Repo: "alice/repo"})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if ctx.Bound.Owner != "alice" || ctx.Bound.Name != "repo" {
		t.Errorf("binding = %+v, want alice/repo", ctx.Bound)
	}
	if ctx.Bound.Instance != "github" {
		t.Errorf("instance = %q, want github", ctx.Bound.Instance)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
