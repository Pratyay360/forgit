package gitlocal

import "testing"

// TestParseForgePath covers the per-forge URL shapes that matter for binding
// a local remote to a configured instance.
func TestParseForgePath(t *testing.T) {
	cases := []struct {
		forge, url  string
		owner, name string
		ok          bool
	}{
		{"github", "https://github.com/alice/repo.git", "alice", "repo", true},
		{"github", "git@github.com:alice/repo.git", "alice", "repo", true},
		{"gitlab", "https://gitlab.com/group/sub/repo.git", "group/sub", "repo", true},
		{"gitlab", "https://gitlab.example.com/foo/bar/baz.git", "foo/bar", "baz", true},
		{"forgejo", "https://codeberg.org/alice/repo.git", "alice", "repo", true},
		{"sourcehut", "https://git.sr.ht/~alice/repo", "alice", "repo", true},
		{"sourcehut", "git@git.sr.ht:~alice/repo", "alice", "repo", true},
		{"bitbucket", "https://bitbucket.org/workspace/repo.git", "workspace", "repo", true},
		// Local paths have no host, so the binding never reaches parseForgePath;
		// these are left out of the table deliberately.
	}
	for _, c := range cases {
		t.Run(c.forge+"_"+c.url, func(t *testing.T) {
			owner, name, ok := parseForgePath(c.forge, c.url)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if owner != c.owner || name != c.name {
				t.Errorf("got %q/%q, want %q/%q", owner, name, c.owner, c.name)
			}
		})
	}
}

// TestSplitRepoName keeps nested groups together for GitLab-style slugs while
// treating the last segment as the repo name.
func TestSplitRepoName(t *testing.T) {
	cases := []struct {
		forge, in, owner, name string
	}{
		{"gitlab", "group/sub/repo", "group/sub", "repo"},
		{"github", "alice/repo", "alice", "repo"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			owner, name := splitRepoName(c.in, c.forge)
			if owner != c.owner || name != c.name {
				t.Errorf("got %q/%q, want %q/%q", owner, name, c.owner, c.name)
			}
		})
	}
}

// TestURLHost covers both URL and scp-style remote forms.
func TestURLHost(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://github.com/alice/repo.git", "github.com"},
		{"ssh://git@gitlab.com/alice/repo.git", "gitlab.com"},
		{"git@bitbucket.org:workspace/repo.git", "bitbucket.org"},
		{"", ""},
		{"/local/path", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := urlHost(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
