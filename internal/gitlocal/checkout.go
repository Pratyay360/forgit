package gitlocal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/pratyay360/forgit/operations"
)

// CheckoutOptions controls CheckoutPR.
type CheckoutOptions struct {
	// Spec describes what to fetch.
	Spec operations.FetchSpec
	// Detach checks out the head commit without creating a branch.
	Detach bool
	// Force resets an existing local branch to the fetched head instead of
	// failing.
	Force bool
}

// CheckoutPR fetches a remote pull request head and checks it out locally.
//
// The fetch is targeted: it downloads only the refs named in the spec, so it is
// cheap even for a fork's pull request that shares little history with the
// local branches. When the spec names no refspecs the remote's default fetch
// refspec is used instead.
func (r *Repo) CheckoutPR(ctx context.Context, auth Authenticator, opts CheckoutOptions) error {
	remote := opts.Spec.Remote
	if remote == "" {
		remote = "origin"
	}
	rem, err := r.repo.Remote(remote)
	if err != nil {
		return fmt.Errorf("no remote named %q: %w", remote, err)
	}
	remoteURL := ""
	if urls := rem.Config().URLs; len(urls) > 0 {
		remoteURL = urls[0]
	}

	fetch := &git.FetchOptions{
		RemoteName: remote,
		Auth:       auth,
	}
	if len(opts.Spec.Refspecs) > 0 {
		specs := make([]config.RefSpec, 0, len(opts.Spec.Refspecs))
		for _, s := range opts.Spec.Refspecs {
			spec := config.RefSpec(s)
			if err := spec.Validate(); err != nil {
				return fmt.Errorf("invalid refspec %q: %w", s, err)
			}
			specs = append(specs, spec)
		}
		fetch.RefSpecs = specs
		// A targeted fetch must not prune: the refs are synthetic (for example
		// refs/pull/12/head) and are not covered by the remote's refspec.
		fetch.Prune = false
	}
	if err := rem.FetchContext(ctx, fetch); err != nil && !isNoUpdates(err) {
		return authError(fmt.Errorf("fetching pull request head: %w", err), remoteURL)
	}

	head, err := r.resolveHead(remote, opts.Spec)
	if err != nil {
		return err
	}

	wt, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("opening worktree: %w", err)
	}

	if opts.Detach {
		if err := wt.Checkout(&git.CheckoutOptions{Hash: *head}); err != nil {
			return fmt.Errorf("checking out %s: %w", shortHash(head), err)
		}
		return nil
	}

	branch := opts.Spec.LocalBranch
	if branch == "" {
		return fmt.Errorf("no local branch name for this pull request; pass --branch")
	}
	return r.checkoutBranch(wt, branch, *head, opts.Force)
}

// headRef returns the remote-side ref name whose fetched value is the head
// commit. Refspec destinations are the remote-tracking refs go-git writes.
func headRef(spec operations.FetchSpec) string {
	if len(spec.Refspecs) == 0 {
		return "HEAD"
	}
	last := spec.Refspecs[len(spec.Refspecs)-1]
	if i := strings.Index(last, ":"); i >= 0 && i+1 < len(last) {
		return strings.TrimPrefix(last[i+1:], "refs/remotes/")
	}
	return last
}

// resolveHead finds the fetched commit for the pull request, preferring the
// head SHA reported by the forge so the checkout is pinned to the exact commit
// the API described.
func (r *Repo) resolveHead(remote string, spec operations.FetchSpec) (*plumbing.Hash, error) {
	if spec.HeadSHA != "" {
		h := plumbing.NewHash(spec.HeadSHA)
		// The fetch may not have contained the object if the refspec resolved
		// to an existing local ref; confirm before trusting it.
		if _, err := r.repo.CommitObject(h); err == nil {
			return &h, nil
		}
	}

	refName := plumbing.NewRemoteReferenceName(remote, headRef(spec))
	ref, err := r.repo.Reference(refName, true)
	if err != nil {
		if spec.HeadSHA != "" {
			// The SHA is known but the object never arrived, which means the
			// branch was deleted or force-pushed away after we read the API.
			return nil, fmt.Errorf("head commit %s for this pull request is not in the local repository; the branch may have been deleted or force-pushed", shortString(spec.HeadSHA))
		}
		return nil, fmt.Errorf("fetched refs for this pull request are missing; the forge may have deleted the head branch")
	}
	h := ref.Hash()
	return &h, nil
}

// checkoutBranch points branch at head, creating it when absent. An existing
// branch already sitting on head is left alone, so re-checking out the same
// pull request is harmless.
func (r *Repo) checkoutBranch(wt *git.Worktree, branch string, head plumbing.Hash, force bool) error {
	refName := plumbing.NewBranchReferenceName(branch)
	existing, err := r.repo.Reference(refName, true)
	exists := err == nil

	if exists && !force {
		if existing.Hash() == head {
			return checkoutRef(wt, branch)
		}
		// Fast-forward when the existing branch is an ancestor of the head, so
		// re-checking out an updated pull request simply moves along.
		if ok, err := r.isAncestor(existing.Hash(), head); err == nil && ok {
			if err := wt.Reset(&git.ResetOptions{Commit: head, Mode: git.HardReset}); err != nil {
				return fmt.Errorf("updating %s to %s: %w", branch, shortHash(&head), err)
			}
			return checkoutRef(wt, branch)
		}
		return fmt.Errorf("local branch %q already exists at a different commit; pass --force to reset it", branch)
	}

	if exists {
		// Drop the old tip so the checkout below can recreate the branch.
		_ = r.repo.Storer.RemoveReference(refName)
	}

	if err := r.repo.Storer.SetReference(plumbing.NewHashReference(refName, head)); err != nil {
		return fmt.Errorf("creating branch %s: %w", branch, err)
	}
	if err := checkoutRef(wt, branch); err != nil {
		// Undo the branch creation so a failed checkout leaves no stray branch.
		_ = r.repo.Storer.RemoveReference(refName)
		return err
	}
	return nil
}

// checkoutRef switches the worktree to an existing branch.
func checkoutRef(wt *git.Worktree, branch string) error {
	if err := wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(branch)}); err != nil {
		return fmt.Errorf("checking out %s: %w", branch, err)
	}
	return nil
}

// isAncestor reports whether old is reachable from newer.
func (r *Repo) isAncestor(old, newer plumbing.Hash) (bool, error) {
	iter, err := r.repo.Log(&git.LogOptions{From: newer})
	if err != nil {
		return false, err
	}
	defer iter.Close()

	found := false
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Hash == old {
			found = true
			return storer.ErrStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		return false, err
	}
	return found, nil
}

// isNoUpdates reports whether a fetch error simply means the remote had nothing
// new, which is not a failure.
func isNoUpdates(err error) bool {
	return err == git.NoErrAlreadyUpToDate
}

func shortHash(h *plumbing.Hash) string { return shortString(h.String()) }

func shortString(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
