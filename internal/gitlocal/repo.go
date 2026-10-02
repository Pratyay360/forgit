// Package gitlocal provides the local git operations forgit needs: opening a
// repository, mapping its remotes back to a configured forge instance, and
// checking out a remote pull request into a local branch.
package gitlocal

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Repo is an opened local git repository.
type Repo struct {
	repo *git.Repository
	// path is the working tree root. It is empty for bare repositories.
	path string
}

// Open opens the repository containing dir, walking up parent directories the
// way git does when run inside a subdirectory of a worktree.
func Open(dir string) (*Repo, error) {
	r, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		if err == git.ErrRepositoryNotExists {
			return nil, fmt.Errorf("not inside a git repository (looked in %s and its parents); run this inside a clone, or pass --repo and --instance to target a forge without a local clone", dir)
		}
		return nil, fmt.Errorf("opening repository: %w", err)
	}
	out := &Repo{repo: r}
	if wt, err := r.Worktree(); err == nil {
		// PlainOpen returns the repository root when DetectDotGit walked up, so
		// recording the worktree filesystem root keeps path joins predictable.
		if fs := wt.Filesystem.Root(); fs != "" {
			out.path = fs
		}
	}
	return out, nil
}

// Exists reports whether dir is inside a git repository, without opening it.
func Exists(dir string) bool {
	_, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	return err == nil
}

// Root returns the working tree root path, or "" for a bare repository.
func (r *Repo) Root() string { return r.path }

// Git returns the underlying go-git repository.
func (r *Repo) Git() *git.Repository { return r.repo }

// Worktree returns the repository's main worktree.
func (r *Repo) Worktree() (*git.Worktree, error) { return r.repo.Worktree() }

// Remote describes a configured remote.
type Remote struct {
	Name string
	// URLs holds the remote's fetch URLs and, when present, its push URLs.
	URLs []string
}

// URL returns the first configured URL, or "".
func (r Remote) URL() string {
	if len(r.URLs) == 0 {
		return ""
	}
	return r.URLs[0]
}

// Remotes lists the repository's configured remotes.
func (r *Repo) Remotes() ([]Remote, error) {
	list, err := r.repo.Remotes()
	if err != nil {
		return nil, fmt.Errorf("listing remotes: %w", err)
	}
	out := make([]Remote, 0, len(list))
	for _, rem := range list {
		out = append(out, Remote{Name: rem.Config().Name, URLs: rem.Config().URLs})
	}
	return out, nil
}

// Head returns the current HEAD reference.
func (r *Repo) Head() (*plumbing.Reference, error) { return r.repo.Head() }

// HeadBranch returns the short name of the current branch, or "" when HEAD is
// detached.
func (r *Repo) HeadBranch() string {
	ref, err := r.repo.Head()
	if err != nil || !ref.Name().IsBranch() {
		return ""
	}
	return ref.Name().Short()
}

// CurrentBranch returns the branch the worktree is on. It returns an error when
// HEAD is detached, which callers surface as a hint to name a branch.
func (r *Repo) CurrentBranch() (string, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("reading HEAD: %w", err)
	}
	if !ref.Name().IsBranch() {
		return "", fmt.Errorf("HEAD is detached at %s; check out a branch first or pass --base/--head explicitly", ref.Hash().String()[:7])
	}
	return ref.Name().Short(), nil
}

// ResolveRev resolves a revision (branch, tag, short hash or a ref such as
// refs/pull/12/head) to a commit hash.
func (r *Repo) ResolveRev(rev string) (plumbing.Hash, error) {
	h, err := r.repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolving %q: %w", rev, err)
	}
	return *h, nil
}

// CommitObject loads a commit by hash.
func (r *Repo) CommitObject(h plumbing.Hash) (*object.Commit, error) {
	c, err := r.repo.CommitObject(h)
	if err != nil {
		return nil, fmt.Errorf("loading commit %s: %w", shortString(h.String()), err)
	}
	return c, nil
}

// AddRemote registers a new remote pointing at url. It returns an error when a
// remote with this name already exists, matching git's own behaviour.
func (r *Repo) AddRemote(name, url string) error {
	_, err := r.repo.CreateRemote(&config.RemoteConfig{Name: name, URLs: []string{url}})
	return err
}

// RemoveRemote deletes a remote by name. Unknown names are reported, so they do
// not silently leak between invocations.
func (r *Repo) RemoveRemote(name string) error {
	return r.repo.DeleteRemote(name)
}
