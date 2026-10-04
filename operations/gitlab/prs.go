package gitlab

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pratyay360/forgit/operations"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// timeOrZero returns the time, or the zero value when the SDK left it nil.
func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// GetPR returns a single merge request in full.
func (c *Client) GetPR(ctx context.Context, repo string, number int) (operations.PRDetail, error) {
	mr, _, err := c.inner.MergeRequests.GetMergeRequest(repo, int64(number), nil)
	if err != nil {
		return operations.PRDetail{}, fmt.Errorf("getting merge request: %w", err)
	}
	return c.prDetail(repo, mr), nil
}

// prDetail converts an API merge request into the unified detail type.
func (c *Client) prDetail(repo string, mr *gitlab.MergeRequest) operations.PRDetail {
	d := operations.PRDetail{
		Forge:      c.forgeLabelOrDefault(),
		Instance:   c.Name(),
		Repo:       repo,
		Number:     int(mr.IID),
		Title:      mr.Title,
		State:      mr.State,
		URL:        mr.WebURL,
		Body:       mr.Description,
		Draft:      mr.Draft,
		Merged:     mr.MergedAt != nil,
		HeadBranch: mr.SourceBranch,
		HeadSHA:    mr.SHA,
		BaseBranch: mr.TargetBranch,
		CreatedAt:  timeOrZero(mr.CreatedAt),
		UpdatedAt:  timeOrZero(mr.UpdatedAt),
	}
	if mr.Author != nil {
		d.Author = mr.Author.Username
	}
	if mr.SourceProjectID != 0 && mr.SourceProjectID != mr.TargetProjectID {
		// Cross-project merge request: the head lives in a fork, so the owner
		// is recorded as the source project so the branch name still varies.
		d.HeadOwner = fmt.Sprintf("project-%d", mr.SourceProjectID)
	}
	d.Labels = append(d.Labels, mr.Labels...)
	return d
}

// CreatePR opens a merge request.
func (c *Client) CreatePR(ctx context.Context, in operations.PRInput) (operations.PRDetail, error) {
	opts := &gitlab.CreateMergeRequestOptions{
		Title:       gitlab.Ptr(in.Title),
		Description: gitlab.Ptr(in.Body),
	}
	if in.Base != "" {
		opts.TargetBranch = gitlab.Ptr(in.Base)
	}
	if in.Head != "" {
		opts.SourceBranch = gitlab.Ptr(in.Head)
	}
	if in.Draft {
		// GitLab only accepts draft on the create form; the API field exists
		// but is validated against the free-form title instead, so the "Draft:"
		// prefix is what actually marks it as a draft in older versions.
		opts.Title = gitlab.Ptr("Draft: " + in.Title)
	}
	mr, _, err := c.inner.MergeRequests.CreateMergeRequest(in.Repo, opts)
	if err != nil {
		return operations.PRDetail{}, fmt.Errorf("creating merge request: %w", err)
	}
	return c.prDetail(in.Repo, mr), nil
}

// CommentPR adds a note to a merge request.
func (c *Client) CommentPR(ctx context.Context, repo string, number int, body string) error {
	_, _, err := c.inner.Notes.CreateMergeRequestNote(repo, int64(number), &gitlab.CreateMergeRequestNoteOptions{
		Body: gitlab.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("commenting on merge request: %w", err)
	}
	return nil
}

// ClosePR closes a merge request without merging it.
func (c *Client) ClosePR(ctx context.Context, repo string, number int) error {
	state := "closed"
	_, _, err := c.inner.MergeRequests.UpdateMergeRequest(repo, int64(number), &gitlab.UpdateMergeRequestOptions{
		StateEvent: &state,
	})
	if err != nil {
		return fmt.Errorf("closing merge request: %w", err)
	}
	return nil
}

// MergePR merges a merge request.
//
// GitLab does not accept a merge strategy per request: the merge method is a
// project setting (merge commit, fast-forward or rebase). Only squash can be
// requested per merge, so that is the only method acted on here; the others fall
// back to whatever the project is configured to do.
func (c *Client) MergePR(ctx context.Context, repo string, number int, opts operations.MergeOptions) error {
	accept := &gitlab.AcceptMergeRequestOptions{}
	if opts.MergeMethod == "squash" {
		accept.Squash = gitlab.Ptr(true)
	}
	if opts.DeleteBranch {
		accept.ShouldRemoveSourceBranch = gitlab.Ptr(true)
	}
	msg := opts.Title
	if msg == "" {
		msg = opts.OptionalMessage
	}
	if msg != "" {
		accept.MergeCommitMessage = gitlab.Ptr(msg)
	}

	mr, _, err := c.inner.MergeRequests.AcceptMergeRequest(repo, int64(number), accept)
	if err != nil {
		return fmt.Errorf("merging merge request: %w", err)
	}
	if mr != nil && mr.State != "merged" {
		return fmt.Errorf("merge was not completed: merge request is %s", mr.State)
	}
	return nil
}

// SetPRReady toggles a merge request between draft and ready.
//
// GitLab models the draft state as a "Draft:" prefix on the title rather than
// as a settable flag, so the title is rewritten. This is the same approach glab
// takes, and it keeps working on self-hosted versions that predate the draft
// field on the API.
func (c *Client) SetPRReady(ctx context.Context, repo string, number int, ready bool) error {
	mr, err := c.GetPR(ctx, repo, number)
	if err != nil {
		return err
	}
	updated := setDraftTitle(mr.Title, ready)
	if updated == mr.Title {
		return nil // already in the requested state
	}
	if _, _, err := c.inner.MergeRequests.UpdateMergeRequest(repo, int64(number), &gitlab.UpdateMergeRequestOptions{
		Title: gitlab.Ptr(updated),
	}); err != nil {
		return fmt.Errorf("setting merge request ready state: %w", err)
	}
	return nil
}

// draftPrefix is GitLab's marker for a draft merge request title.
const draftPrefix = "Draft:"

// setDraftTitle adds or removes the draft prefix.
func setDraftTitle(title string, ready bool) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(title), draftPrefix))
	if ready {
		return trimmed
	}
	return draftPrefix + " " + trimmed
}

// PRDiff returns the unified diff of a merge request.
func (c *Client) PRDiff(ctx context.Context, repo string, number int) (string, error) {
	raw, _, err := c.inner.MergeRequests.ShowMergeRequestRawDiffs(repo, int64(number), nil)
	if err != nil {
		return "", fmt.Errorf("fetching merge request diff: %w", err)
	}
	return string(raw), nil
}

// FetchSpec returns how to fetch a merge request head locally.
//
// GitLab publishes every merge request head under refs/merge-requests/N/head,
// which also exists for fork merge requests.
func (c *Client) FetchSpec(ctx context.Context, repo string, number int) (operations.FetchSpec, error) {
	mr, err := c.GetPR(ctx, repo, number)
	if err != nil {
		return operations.FetchSpec{}, err
	}
	if mr.HeadSHA == "" {
		return operations.FetchSpec{}, fmt.Errorf("merge request %d has no resolvable head commit", number)
	}
	ref := fmt.Sprintf("refs/merge-requests/%d/head", number)
	return operations.FetchSpec{
		Remote:      "origin",
		Refspecs:    []string{fmt.Sprintf("+%s:%s", ref, ref)},
		LocalBranch: glabBranchName(mr),
		HeadSHA:     mr.HeadSHA,
	}, nil
}

// glabBranchName builds the local branch name, following glab's convention of
// prefixing with the merge request IID.
func glabBranchName(mr operations.PRDetail) string {
	return fmt.Sprintf("%d-%s", mr.Number, mr.HeadBranch)
}

// GetIssue returns a single issue in full.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (operations.IssueDetail, error) {
	is, _, err := c.inner.Issues.GetIssue(repo, int64(number), nil)
	if err != nil {
		return operations.IssueDetail{}, fmt.Errorf("getting issue: %w", err)
	}
	return c.issueDetail(repo, is), nil
}

// issueDetail converts an API issue into the unified detail type.
func (c *Client) issueDetail(repo string, is *gitlab.Issue) operations.IssueDetail {
	d := operations.IssueDetail{
		Forge:     c.forgeLabelOrDefault(),
		Instance:  c.Name(),
		Repo:      repo,
		Number:    int(is.IID),
		Title:     is.Title,
		State:     is.State,
		URL:       is.WebURL,
		Body:      is.Description,
		CreatedAt: timeOrZero(is.CreatedAt),
		UpdatedAt: timeOrZero(is.UpdatedAt),
	}
	if is.Author != nil {
		d.Author = is.Author.Username
	}
	d.Labels = append(d.Labels, is.Labels...)
	for _, a := range is.Assignees {
		d.Assignees = append(d.Assignees, a.Username)
	}
	return d
}

// CreateIssue opens a new issue.
func (c *Client) CreateIssue(ctx context.Context, in operations.IssueInput) (operations.IssueDetail, error) {
	opts := &gitlab.CreateIssueOptions{
		Title:       gitlab.Ptr(in.Title),
		Description: gitlab.Ptr(in.Body),
	}
	if len(in.Labels) > 0 {
		labels := gitlab.LabelOptions(in.Labels)
		opts.Labels = &labels
	}
	is, _, err := c.inner.Issues.CreateIssue(in.Repo, opts)
	if err != nil {
		return operations.IssueDetail{}, fmt.Errorf("creating issue: %w", err)
	}
	return c.issueDetail(in.Repo, is), nil
}

// CommentIssue adds a note to an issue.
func (c *Client) CommentIssue(ctx context.Context, repo string, number int, body string) error {
	_, _, err := c.inner.Notes.CreateIssueNote(repo, int64(number), &gitlab.CreateIssueNoteOptions{
		Body: gitlab.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("commenting on issue: %w", err)
	}
	return nil
}

// CloseIssue closes an issue.
func (c *Client) CloseIssue(ctx context.Context, repo string, number int) error {
	state := "closed"
	if _, _, err := c.inner.Issues.UpdateIssue(repo, int64(number), &gitlab.UpdateIssueOptions{
		StateEvent: &state,
	}); err != nil {
		return fmt.Errorf("closing issue: %w", err)
	}
	return nil
}
