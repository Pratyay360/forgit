package operations

import "time"

// PRDetail is the full view of a single pull request, merge request or
// patchset. It extends the listing-level PR with the fields needed to act on
// it: the head commit for checking out, the draft state, and the base branch
// for merging.
type PRDetail struct {
	Forge    string
	Instance string
	Repo     string
	Number   int
	Title    string
	State    string // open, closed, merged
	URL      string
	Body     string

	Draft  bool
	Merged bool

	Author string
	Labels []string

	HeadBranch string
	HeadSHA    string
	HeadOwner  string // owner of the head repository, for cross-fork PRs
	// HeadRepoName is the head repository's slug without its namespace. It is
	// used to build a fork clone URL on forges that have no pull request ref
	// namespace, where the head branch must be fetched from the fork.
	HeadRepoName string
	BaseBranch   string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IssueDetail is the full view of a single issue or ticket.
type IssueDetail struct {
	Forge    string
	Instance string
	Repo     string
	Number   int
	Title    string
	State    string // open, closed
	URL      string
	Body     string

	Author string
	Labels []string

	Assignees []string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// FetchSpec describes how to obtain a remote ref in a local repository. Forge
// clients return a spec rather than a ready-made ref because forges expose
// pull request heads differently: GitHub and Forgejo publish a stable
// refs/pull/N/head namespace, GitLab publishes refs/merge-requests/N/head, and
// Bitbucket and SourceHut have no pull request ref namespace at all, so their
// clients resolve the head commit through the API instead.
type FetchSpec struct {
	// Remote is the local repository remote name to fetch from.
	Remote string
	// Refspecs are the refspecs to fetch. Fetching an empty list falls back to
	// the remote's configured fetch refspec.
	Refspecs []string
	// LocalBranch is the branch name to create for the pull request. Ignored
	// when the caller asks to detach.
	LocalBranch string
	// HeadSHA is the resolved head commit, used to verify the checkout landed on
	// the commit the forge reports.
	HeadSHA string
	// ForkURL is set when the head branch exists only on a fork, so the fetch
	// must target that clone URL instead of the configured remote. When set, the
	// caller registers it as an additional remote before fetching.
	ForkURL string
}

// PRInput describes a pull request to create.
type PRInput struct {
	Repo  string
	Title string
	Body  string
	Base  string // base branch; defaults to the repository's default branch
	Head  string // head branch; defaults to the current branch
	Draft bool
}

// IssueInput describes an issue to create.
type IssueInput struct {
	Repo  string
	Title string
	Body  string
	// Labels are label names to apply, when the forge supports labels.
	Labels []string
}

// MergeOptions controls how a pull request is merged.
type MergeOptions struct {
	// MergeMethod is one of merge, squash or rebase. Forges that do not offer
	// a choice ignore it.
	MergeMethod string
	// DeleteBranch asks the forge to delete the head branch after merging.
	DeleteBranch bool
	// Title and OptionalMessage override the generated merge commit message.
	Title           string
	OptionalMessage string
}
