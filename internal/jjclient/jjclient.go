// Package jjclient wraps the `jj` CLI for local git operations,
// replacing the go-git library previously used. jj is a Git-compatible
// SCM that exposes all operations through its subcommand interface.
package jjclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pratyay360/forgit/operations"
)

// Repo is an opened local repository.
type Repo struct {
	path string
}

func Open(dir string) (*Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	if !jjWorkspace(abs) {
		return nil, fmt.Errorf("not inside a jj workspace (looked in %s and its parents)", dir)
	}
	return &Repo{path: abs}, nil
}

// Exists reports whether dir is inside a jj workspace.
func Exists(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return jjWorkspace(abs)
}

// jjWorkspace checks whether dir or any parent contains a .jj directory.
func jjWorkspace(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".jj")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// Root returns the repository root path.
func (r *Repo) Root() string { return r.path }

// Remote describes a configured remote.
type Remote struct {
	Name string
	URLs []string
}

// URL returns the first configured URL, or "".
func (r Remote) URL() string {
	if len(r.URLs) == 0 {
		return ""
	}
	return r.URLs[0]
}

// Remotes lists the repository's configured remotes via `jj remote list`.
func (r *Repo) Remotes() ([]Remote, error) {
	out, err := r.jj("remote", "list")
	if err != nil {
		return nil, err
	}
	var remotes []Remote
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		remotes = append(remotes, Remote{Name: parts[0], URLs: strings.Fields(parts[1])})
	}
	return remotes, nil
}

// Head returns the current HEAD reference.
func (r *Repo) Head() (*plumbingReference, error) {
	out, err := r.jj("log", "-r", "@", "-T", "commit_id")
	if err != nil {
		return nil, err
	}
	hash := strings.TrimSpace(out)
	return &plumbingReference{name: "HEAD", hash: hash}, nil
}

// HeadBranch returns the short name of the current branch, or "" when HEAD is detached.
func (r *Repo) HeadBranch() string {
	out, err := r.jj("branch", "list", "-r", "@")
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return ""
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

// CurrentBranch returns the branch the worktree is on.
func (r *Repo) CurrentBranch() (string, error) {
	out, err := r.jj("branch", "list", "-r", "@")
	if err != nil {
		return "", fmt.Errorf("reading current branch: %w", err)
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return "", fmt.Errorf("HEAD is detached; check out a branch first")
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("HEAD is detached; check out a branch first")
	}
	return strings.TrimSpace(parts[0]), nil
}

// ResolveRev resolves a revision to a commit hash.
func (r *Repo) ResolveRev(rev string) (string, error) {
	out, err := r.jj("log", "-r", rev, "-T", "commit_id")
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", rev, err)
	}
	return strings.TrimSpace(out), nil
}

// CommitObject loads a commit by hash.
func (r *Repo) CommitObject(hash string) (*Commit, error) {
	out, err := r.jj("log", "-r", hash, "-T", "{commit_id} {author_name} {author_email} {commit_description}")
	if err != nil {
		return nil, fmt.Errorf("loading commit %s: %w", shortHash(hash), err)
	}
	c, err := parseCommitLine(out)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// AddRemote registers a new remote pointing at url.
func (r *Repo) AddRemote(name, url string) error {
	_, err := r.jj("remote", "add", name, url)
	return err
}

// RemoveRemote deletes a remote by name.
func (r *Repo) RemoveRemote(name string) error {
	_, err := r.jj("remote", "remove", name)
	return err
}

// CheckoutOptions controls CheckoutPR.
type CheckoutOptions struct {
	Spec   operations.FetchSpec
	Detach bool
	Force  bool
}

// CheckoutPR fetches a remote pull request head and checks it out locally.
func (r *Repo) CheckoutPR(ctx context.Context, auth Authenticator, opts CheckoutOptions) error {
	remote := opts.Spec.Remote
	if remote == "" {
		remote = "origin"
	}

	// Fetch the PR ref.
	if len(opts.Spec.Refspecs) > 0 {
		for _, spec := range opts.Spec.Refspecs {
			if _, err := r.jj("git", "fetch", remote, spec); err != nil {
				return fmt.Errorf("fetching pull request head: %w", err)
			}
		}
	} else {
		if _, err := r.jj("git", "fetch", remote); err != nil {
			return fmt.Errorf("fetching pull request head: %w", err)
		}
	}

	head, err := r.resolveHead(remote, opts.Spec)
	if err != nil {
		return err
	}

	branch := opts.Spec.LocalBranch
	if branch == "" {
		return fmt.Errorf("no local branch name for this pull request; pass --branch")
	}

	// Check out the commit.
	if _, err := r.jj("new", head, "-b", branch); err != nil {
		return fmt.Errorf("checking out %s: %w", branch, err)
	}
	return nil
}

func (r *Repo) resolveHead(_ string, spec operations.FetchSpec) (string, error) {
	if spec.HeadSHA != "" {
		// Verify the SHA is available locally.
		if _, err := r.jj("log", "-r", spec.HeadSHA); err == nil {
			return spec.HeadSHA, nil
		}
		return "", fmt.Errorf("head commit %s for this pull request is not in the local repository; the branch may have been deleted or force-pushed", shortString(spec.HeadSHA))
	}
	// Use the fetched ref directly.
	ref := spec.Refspecs[len(spec.Refspecs)-1]
	// Parse the destination ref from the refspec.
	parts := strings.Split(ref, ":")
	if len(parts) == 2 {
		return parts[1], nil
	}
	return "", fmt.Errorf("fetched refs for this pull request are missing")
}

// Status returns the worktree status relative to HEAD.
func (r *Repo) Status() ([]Change, error) {
	out, err := r.jj("status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("reading status: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return []Change{}, nil
	}
	var changes []Change
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		staging := fields[0]
		worktree := fields[1]
		path := fields[len(fields)-1]
		c := Change{
			Path:     path,
			Staging:  statusCode(staging),
			Worktree: statusCode(worktree),
		}
		if worktree == "?" {
			c.Untracked = true
		}
		changes = append(changes, c)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
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
	args := []string{"log", "-r", "@", "-T", "{commit_id} {author_name} {author_email} {commit_description}"}
	if limit > 0 {
		args = append(args, "-n", fmt.Sprintf("%d", limit))
	}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := r.jj(args...)
	if err != nil {
		return nil, fmt.Errorf("reading history: %w", err)
	}
	return parseLogEntries(out)
}

// Diff renders the unified diff between two revisions.
func (r *Repo) Diff(from, to string) (string, error) {
	args := []string{"diff"}
	if from != "" {
		args = append(args, from)
	}
	if to != "" {
		args = append(args, to)
	}
	out, err := r.jj(args...)
	if err != nil {
		return "", fmt.Errorf("computing diff: %w", err)
	}
	return out, nil
}

// Branches lists local and remote-tracking branches.
type Branch struct {
	Name     string
	Short    string
	Commit   string
	IsHead   bool
	IsRemote bool
}

// Branches returns the repository's branches, local first then remote, sorted by name.
func (r *Repo) Branches() ([]Branch, error) {
	head, err := r.Head()
	var headName string
	if err == nil {
		headName = head.hash
	}

	out, err := r.jj("branch", "list", "-r", "--all")
	if err != nil {
		return nil, fmt.Errorf("listing branches: %w", err)
	}

	var outBranches []Branch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		hash := strings.TrimSpace(parts[1])
		isRemote := strings.HasPrefix(name, "remote/")
		short := name
		if isRemote {
			short = strings.TrimPrefix(name, "remote/")
		}
		outBranches = append(outBranches, Branch{
			Name:     name,
			Short:    short,
			Commit:   hash,
			IsHead:   hash == headName,
			IsRemote: isRemote,
		})
	}

	sort.Slice(outBranches, func(i, j int) bool {
		if outBranches[i].IsRemote != outBranches[j].IsRemote {
			return !outBranches[i].IsRemote
		}
		return outBranches[i].Short < outBranches[j].Short
	})
	return outBranches, nil
}

// runJj executes a jj command and returns stdout.
func (r *Repo) jj(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "jj", args...)
	cmd.Dir = r.path
	cmd.Env = os.Environ()
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("jj %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// plumbingReference represents a git reference.
type plumbingReference struct {
	name string
	hash string
}

// Commit represents a parsed commit.
type Commit struct {
	Hash    string
	Author  string
	Email   string
	Subject string
}

// parseCommitLine parses a jj log template output line into a Commit.
func parseCommitLine(out string) (*Commit, error) {
	line := strings.TrimSpace(out)
	if line == "" {
		return nil, fmt.Errorf("empty commit output")
	}
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return nil, fmt.Errorf("unexpected commit format: %s", line)
	}
	return &Commit{
		Hash:    parts[0],
		Author:  parts[1],
		Email:   parts[2],
		Subject: parts[3],
	}, nil
}

// parseLogEntries parses jj log template output into LogEntry slices.
func parseLogEntries(out string) ([]LogEntry, error) {
	var entries []LogEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}
		entries = append(entries, LogEntry{
			Hash:    parts[0],
			Short:   shortString(parts[0]),
			Author:  parts[1],
			Email:   parts[2],
			Subject: parts[3],
		})
	}
	return entries, nil
}

func shortHash(hash string) string { return shortString(hash) }

func shortString(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func statusCode(s string) StatusCode {
	switch s {
	case "M":
		return StatusModified
	case "A":
		return StatusAdded
	case "D":
		return StatusDeleted
	case "R":
		return StatusRenamed
	case "C":
		return StatusCopied
	case "?":
		return StatusUntracked
	default:
		return StatusUnmodified
	}
}
