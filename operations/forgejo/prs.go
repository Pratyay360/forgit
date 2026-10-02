package forgejo

import (
	"context"
	"fmt"
	"time"

	fj "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"
	"github.com/pratyay360/forgit/operations"
)

// GetPR returns a single pull request in full.
func (c *Client) GetPR(ctx context.Context, repo string, number int) (operations.PRDetail, error) {
	owner, name, ok := splitFullName(repo)
	if !ok {
		return operations.PRDetail{}, fmt.Errorf("repository must be owner/name, got %q", repo)
	}
	pr, _, err := c.inner.GetPullRequest(owner, name, int64(number))
	if err != nil {
		return operations.PRDetail{}, fmt.Errorf("getting pull request: %w", err)
	}
	return c.prDetail(repo, pr), nil
}

// prDetail converts an API pull request into the unified detail type.
func (c *Client) prDetail(repo string, pr *fj.PullRequest) operations.PRDetail {
	d := operations.PRDetail{
		Forge:     "forgejo",
		Instance:  c.Name(),
		Repo:      repo,
		Number:    int(pr.Index),
		Title:     pr.Title,
		State:     string(pr.State),
		URL:       pr.HTMLURL,
		Body:      pr.Body,
		Merged:    pr.HasMerged,
		CreatedAt: timeOrZero(pr.Created),
		UpdatedAt: timeOrZero(pr.Updated),
	}
	if pr.Poster != nil {
		d.Author = pr.Poster.UserName
	}
	if pr.Head != nil {
		d.HeadBranch = pr.Head.Ref
		d.HeadSHA = pr.Head.Sha
		if pr.Head.Repository != nil {
			d.HeadOwner = pr.Head.Repository.Owner.UserName
		}
	}
	if pr.Base != nil {
		d.BaseBranch = pr.Base.Ref
	}
	for _, l := range pr.Labels {
		d.Labels = append(d.Labels, l.Name)
	}
	return d
}

// CreatePR opens a pull request.
func (c *Client) CreatePR(ctx context.Context, in operations.PRInput) (operations.PRDetail, error) {
	owner, name, ok := splitFullName(in.Repo)
	if !ok {
		return operations.PRDetail{}, fmt.Errorf("repository must be owner/name, got %q", in.Repo)
	}
	pr, _, err := c.inner.CreatePullRequest(owner, name, fj.CreatePullRequestOption{
		Head:  in.Head,
		Base:  in.Base,
		Title: in.Title,
		Body:  in.Body,
	})
	if err != nil {
		return operations.PRDetail{}, fmt.Errorf("creating pull request: %w", err)
	}
	return c.prDetail(in.Repo, pr), nil
}

// CommentPR adds a comment to a pull request.
func (c *Client) CommentPR(ctx context.Context, repo string, number int, body string) error {
	owner, name, ok := splitFullName(repo)
	if !ok {
		return fmt.Errorf("repository must be owner/name, got %q", repo)
	}
	if _, _, err := c.inner.CreateIssueComment(owner, name, int64(number), fj.CreateIssueCommentOption{Body: body}); err != nil {
		return fmt.Errorf("commenting on pull request: %w", err)
	}
	return nil
}

// ClosePR closes a pull request without merging it.
func (c *Client) ClosePR(ctx context.Context, repo string, number int) error {
	owner, name, ok := splitFullName(repo)
	if !ok {
		return fmt.Errorf("repository must be owner/name, got %q", repo)
	}
	state := fj.StateClosed
	if _, _, err := c.inner.EditPullRequest(owner, name, int64(number), fj.EditPullRequestOption{State: &state}); err != nil {
		return fmt.Errorf("closing pull request: %w", err)
	}
	return nil
}

// MergePR merges a pull request.
func (c *Client) MergePR(ctx context.Context, repo string, number int, opts operations.MergeOptions) error {
	owner, name, ok := splitFullName(repo)
	if !ok {
		return fmt.Errorf("repository must be owner/name, got %q", repo)
	}
	merged, _, err := c.inner.MergePullRequest(owner, name, int64(number), fj.MergePullRequestOption{
		Style:                  fjMergeStyle(opts.MergeMethod),
		Title:                  opts.Title,
		Message:                opts.OptionalMessage,
		DeleteBranchAfterMerge: opts.DeleteBranch,
	})
	if err != nil {
		return fmt.Errorf("merging pull request: %w", err)
	}
	if !merged {
		return fmt.Errorf("merge was not completed: the forge refused the merge")
	}
	return nil
}

// fjMergeStyle maps the unified merge method onto Forgejo's vocabulary.
func fjMergeStyle(m string) fj.MergeStyle {
	switch m {
	case "squash":
		return fj.MergeStyleSquash
	case "rebase":
		return fj.MergeStyleRebase
	case "ff":
		return fj.MergeStyleFastForwardOnly
	default:
		return fj.MergeStyleMerge
	}
}

// PRDiff returns the unified diff of a pull request.
func (c *Client) PRDiff(ctx context.Context, repo string, number int) (string, error) {
	owner, name, ok := splitFullName(repo)
	if !ok {
		return "", fmt.Errorf("repository must be owner/name, got %q", repo)
	}
	raw, _, err := c.inner.GetPullRequestDiff(owner, name, int64(number), fj.PullRequestDiffOptions{
		Binary: true,
	})
	if err != nil {
		return "", fmt.Errorf("fetching pull request diff: %w", err)
	}
	return string(raw), nil
}

// FetchSpec returns how to fetch a pull request head locally.
//
// Forgejo mirrors GitHub's refs/pull/N/head namespace, so the head commit is
// fetchable even for pull requests opened from a fork.
func (c *Client) FetchSpec(ctx context.Context, repo string, number int) (operations.FetchSpec, error) {
	pr, err := c.GetPR(ctx, repo, number)
	if err != nil {
		return operations.FetchSpec{}, err
	}
	if pr.HeadSHA == "" {
		return operations.FetchSpec{}, fmt.Errorf("pull request %d has no resolvable head commit", number)
	}
	ref := fmt.Sprintf("refs/pull/%d/head", number)
	return operations.FetchSpec{
		Remote:      "origin",
		Refspecs:    []string{fmt.Sprintf("+%s:%s", ref, ref)},
		LocalBranch: ownerBranchName(pr),
		HeadSHA:     pr.HeadSHA,
	}, nil
}

// ownerBranchName builds the local branch name, prefixing the head owner so
// branches from different contributors stay distinguishable.
func ownerBranchName(pr operations.PRDetail) string {
	if pr.HeadOwner == "" {
		if pr.Author == "" {
			return pr.HeadBranch
		}
		return pr.Author + "-" + pr.HeadBranch
	}
	return pr.HeadOwner + "-" + pr.HeadBranch
}

// GetIssue returns a single issue in full.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (operations.IssueDetail, error) {
	owner, name, ok := splitFullName(repo)
	if !ok {
		return operations.IssueDetail{}, fmt.Errorf("repository must be owner/name, got %q", repo)
	}
	is, _, err := c.inner.GetIssue(owner, name, int64(number))
	if err != nil {
		return operations.IssueDetail{}, fmt.Errorf("getting issue: %w", err)
	}
	return c.issueDetail(repo, is), nil
}

// issueDetail converts an API issue into the unified detail type.
func (c *Client) issueDetail(repo string, is *fj.Issue) operations.IssueDetail {
	d := operations.IssueDetail{
		Forge:     "forgejo",
		Instance:  c.Name(),
		Repo:      repo,
		Number:    int(is.Index),
		Title:     is.Title,
		State:     string(is.State),
		URL:       is.HTMLURL,
		Body:      is.Body,
		CreatedAt: is.Created,
		UpdatedAt: is.Updated,
	}
	if is.Poster != nil {
		d.Author = is.Poster.UserName
	}
	for _, l := range is.Labels {
		d.Labels = append(d.Labels, l.Name)
	}
	for _, a := range is.Assignees {
		d.Assignees = append(d.Assignees, a.UserName)
	}
	return d
}

// CreateIssue opens a new issue.
func (c *Client) CreateIssue(ctx context.Context, in operations.IssueInput) (operations.IssueDetail, error) {
	owner, name, ok := splitFullName(in.Repo)
	if !ok {
		return operations.IssueDetail{}, fmt.Errorf("repository must be owner/name, got %q", in.Repo)
	}
	is, _, err := c.inner.CreateIssue(owner, name, fj.CreateIssueOption{
		Title: in.Title,
		Body:  in.Body,
	})
	if err != nil {
		return operations.IssueDetail{}, fmt.Errorf("creating issue: %w", err)
	}
	return c.issueDetail(in.Repo, is), nil
}

// CommentIssue adds a comment to an issue.
func (c *Client) CommentIssue(ctx context.Context, repo string, number int, body string) error {
	owner, name, ok := splitFullName(repo)
	if !ok {
		return fmt.Errorf("repository must be owner/name, got %q", repo)
	}
	if _, _, err := c.inner.CreateIssueComment(owner, name, int64(number), fj.CreateIssueCommentOption{Body: body}); err != nil {
		return fmt.Errorf("commenting on issue: %w", err)
	}
	return nil
}

// CloseIssue closes an issue.
func (c *Client) CloseIssue(ctx context.Context, repo string, number int) error {
	owner, name, ok := splitFullName(repo)
	if !ok {
		return fmt.Errorf("repository must be owner/name, got %q", repo)
	}
	state := fj.StateClosed
	if _, _, err := c.inner.EditIssue(owner, name, int64(number), fj.EditIssueOption{State: &state}); err != nil {
		return fmt.Errorf("closing issue: %w", err)
	}
	return nil
}

// timeOrZero returns the time, or the zero value when the SDK left it nil.
func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
