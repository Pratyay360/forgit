package gitlocal

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// Change is one file's state in the worktree.
type Change struct {
	Path      string
	Staging   StatusCode // index state
	Worktree  StatusCode // working tree state
	Untracked bool
}

// StatusCode mirrors git's two-letter porcelain status codes. The names are
// prefixed to avoid colliding with the go-git constants of the same meaning.
type StatusCode byte

// Worktree status codes, matching git's porcelain vocabulary.
const (
	StatusUnmodified StatusCode = ' '
	StatusModified   StatusCode = 'M'
	StatusAdded      StatusCode = 'A'
	StatusDeleted    StatusCode = 'D'
	StatusRenamed    StatusCode = 'R'
	StatusCopied     StatusCode = 'C'
	StatusUntracked  StatusCode = '?'
)

// Status returns the worktree status relative to HEAD.
func (r *Repo) Status() ([]Change, error) {
	wt, err := r.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("opening worktree: %w", err)
	}
	st, err := wt.Status()
	if err != nil {
		return nil, fmt.Errorf("reading status: %w", err)
	}
	paths := make([]string, 0, len(st))
	for p := range st {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	out := make([]Change, 0, len(paths))
	for _, p := range paths {
		s := st[p]
		c := Change{Path: p, Staging: statusCode(s.Staging), Worktree: statusCode(s.Worktree)}
		if s.Worktree == git.Untracked {
			c.Untracked = true
		}
		out = append(out, c)
	}
	return out, nil
}

// statusCode maps a go-git status to its porcelain letter. go-git reports
// untracked, unmodified and ignored with dedicated values; the rest map onto
// the ordinary worktree codes.
func statusCode(s git.StatusCode) StatusCode {
	switch s {
	case git.Untracked:
		return StatusUntracked
	case git.Modified:
		return StatusModified
	case git.Added:
		return StatusAdded
	case git.Deleted:
		return StatusDeleted
	case git.Renamed:
		return StatusRenamed
	case git.Copied:
		return StatusCopied
	case git.Unmodified:
		return StatusUnmodified
	default:
		return StatusModified
	}
}

// String renders the change the way git status --short does.
func (c Change) String() string {
	staging := c.Staging
	if c.Untracked {
		return fmt.Sprintf("?? %s", c.Path)
	}
	return fmt.Sprintf("%c%c %s", byte(staging), byte(c.Worktree), c.Path)
}

// LogEntry is one commit in the history listing.
type LogEntry struct {
	Hash    string
	Short   string
	Author  string
	Email   string
	When    time.Time
	Subject string
}

// Log returns up to limit commits reachable from HEAD, newest first.
func (r *Repo) Log(limit int, path string) ([]LogEntry, error) {
	opts := &git.LogOptions{Order: git.LogOrderCommitterTime}
	if path != "" {
		opts.FileName = &path
	}
	iter, err := r.repo.Log(opts)
	if err != nil {
		return nil, fmt.Errorf("reading history: %w", err)
	}
	defer iter.Close()

	var out []LogEntry
	err = iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && len(out) >= limit {
			return errStopIter
		}
		out = append(out, LogEntry{
			Hash:    c.Hash.String(),
			Short:   shortString(c.Hash.String()),
			Author:  c.Author.Name,
			Email:   c.Author.Email,
			When:    c.Author.When,
			Subject: firstLine(c.Message),
		})
		return nil
	})
	if err != nil && !errors.Is(err, errStopIter) {
		return nil, fmt.Errorf("reading history: %w", err)
	}
	return out, nil
}

// errStopIter ends a ForEach walk without reporting a failure. It reuses
// go-git's own sentinel so the comparison stays an errors.Is check.
var errStopIter = storer.ErrStop

// firstLine returns the first line of a commit message, which git log shows as
// the subject.
func firstLine(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return msg[:i]
	}
	return msg
}

// Diff renders the unified diff between two revisions, or between a revision
// and the working tree when to is empty.
func (r *Repo) Diff(from, to string) (string, error) {
	fromHash, err := r.ResolveRev(defaultRev(from))
	if err != nil {
		return "", err
	}
	fromCommit, err := r.repo.CommitObject(fromHash)
	if err != nil {
		return "", fmt.Errorf("loading %s: %w", shortString(fromHash.String()), err)
	}

	var toCommit *object.Commit
	if to != "" {
		toHash, err := r.ResolveRev(defaultRev(to))
		if err != nil {
			return "", err
		}
		if toCommit, err = r.repo.CommitObject(toHash); err != nil {
			return "", fmt.Errorf("loading %s: %w", shortString(toHash.String()), err)
		}
	}

	patch, err := fromCommit.Patch(toCommit)
	if err != nil {
		return "", fmt.Errorf("computing diff: %w", err)
	}
	var b strings.Builder
	if err := writePatch(&b, patch); err != nil {
		return "", err
	}
	return b.String(), nil
}

// defaultRev treats an empty revision as HEAD.
func defaultRev(rev string) string {
	if rev == "" {
		return "HEAD"
	}
	return rev
}

// writePatch renders the file changes of a patch.
func writePatch(b *strings.Builder, p *object.Patch) error {
	if err := p.Encode(b); err != nil {
		return fmt.Errorf("rendering diff: %w", err)
	}
	return nil
}

// Branches lists local and remote-tracking branches.
type Branch struct {
	Name     string
	Short    string
	Commit   string
	IsHead   bool
	IsRemote bool
}

// Branches returns the repository's branches, local first then remote, sorted
// by name.
func (r *Repo) Branches() ([]Branch, error) {
	head, err := r.repo.Head()
	var headName string
	if err == nil && head.Name().IsBranch() {
		headName = head.Name().Short()
	}

	refs, err := r.repo.References()
	if err != nil {
		return nil, fmt.Errorf("listing branches: %w", err)
	}
	defer refs.Close()

	var out []Branch
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name()
		var short string
		switch {
		case name.IsBranch():
			short = name.Short()
		case isRemoteBranch(name):
			short = strings.TrimPrefix(name.Short(), "remotes/")
		default:
			return nil
		}
		out = append(out, Branch{
			Name:     name.String(),
			Short:    short,
			Commit:   ref.Hash().String(),
			IsHead:   short == headName,
			IsRemote: isRemoteBranch(name),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing branches: %w", err)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsRemote != out[j].IsRemote {
			return !out[i].IsRemote
		}
		return out[i].Short < out[j].Short
	})
	return out, nil
}

// isRemoteBranch reports whether a reference is a remote-tracking branch.
func isRemoteBranch(name plumbing.ReferenceName) bool {
	return strings.HasPrefix(name.String(), "refs/remotes/")
}
