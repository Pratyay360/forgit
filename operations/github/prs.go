package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v89/github"
	"github.com/pratyay360/forgit/operations"
)

// GetPR returns a single pull request in full.
func (c *Client) GetPR(ctx context.Context, repo string, number int) (operations.PRDetail, error) {
	owner, name := splitFullName(repo)
	pr, _, err := c.inner.PullRequests.Get(ctx, owner, name, number)
	if err != nil {
		return operations.PRDetail{}, fmt.Errorf("getting pull request: %w", err)
	}
	return c.prDetail(repo, pr), nil
}

// prDetail converts an API pull request into the unified detail type.
func (c *Client) prDetail(repo string, pr *github.PullRequest) operations.PRDetail {
	d := operations.PRDetail{
		Forge:      c.forgeLabelOrDefault(),
		Instance:   c.Name(),
		Repo:       repo,
		Number:     int(deref(pr.Number)),
		Title:      deref(pr.Title),
		State:      deref(pr.State),
		URL:        deref(pr.HTMLURL),
		Body:       deref(pr.Body),
		Draft:      deref(pr.Draft),
		Merged:     deref(pr.Merged),
		HeadBranch: deref(pr.Head.Ref),
		HeadSHA:    deref(pr.Head.SHA),
		BaseBranch: deref(pr.Base.Ref),
	}
	if pr.User != nil {
		d.Author = deref(pr.User.Login)
	}
	if pr.Head.User != nil {
		d.HeadOwner = deref(pr.Head.User.Login)
	}
	for _, l := range pr.Labels {
		d.Labels = append(d.Labels, deref(l.Name))
	}
	if pr.CreatedAt != nil {
		d.CreatedAt = pr.CreatedAt.Time
	}
	if pr.UpdatedAt != nil {
		d.UpdatedAt = pr.UpdatedAt.Time
	}
	return d
}

// CreatePR opens a pull request. An empty Head means the API default, which is
// the current branch's remote counterpart on the base repository.
func (c *Client) CreatePR(ctx context.Context, in operations.PRInput) (operations.PRDetail, error) {
	owner, name := splitFullName(in.Repo)
	pr, _, err := c.inner.PullRequests.Create(ctx, owner, name, &github.NewPullRequest{
		Title: github.Ptr(in.Title),
		Body:  github.Ptr(in.Body),
		Head:  github.Ptr(in.Head),
		Base:  github.Ptr(in.Base),
		Draft: github.Ptr(in.Draft),
	})
	if err != nil {
		return operations.PRDetail{}, fmt.Errorf("creating pull request: %w", err)
	}
	d := c.prDetail(in.Repo, pr)
	if in.Draft && !d.Draft {
		// The REST create endpoint does not honour the draft flag, so the
		// draft is applied with a follow-up call when the API ignored it.
		if err := c.convertToDraft(ctx, owner, name, int(d.Number)); err != nil {
			return operations.PRDetail{}, err
		}
		d.Draft = true
	}
	return d, nil
}

// convertToDraft puts a pull request back into draft state. The endpoint is
// GraphQL-only, so it is issued as a raw REST call.
func (c *Client) convertToDraft(ctx context.Context, owner, name string, number int) error {
	body := map[string]any{"draft": true}
	if err := c.jsonCall(ctx, http.MethodPost, fmt.Sprintf("repos/%s/%s/pulls/%d/convert_to_draft", owner, name, number), body, nil); err != nil {
		return fmt.Errorf("marking pull request as draft: %w", err)
	}
	return nil
}

// markReadyForReview takes a pull request out of draft state.
func (c *Client) markReadyForReview(ctx context.Context, owner, name string, number int) error {
	if err := c.jsonCall(ctx, http.MethodPatch, fmt.Sprintf("repos/%s/%s/pulls/%d/mark_ready_for_review", owner, name, number), nil, nil); err != nil {
		return fmt.Errorf("marking pull request ready for review: %w", err)
	}
	return nil
}

// jsonCall issues an authenticated API request with an optional JSON body.
func (c *Client) jsonCall(ctx context.Context, method, urlStr string, body, out any) error {
	req, err := c.inner.NewRequest(ctx, method, urlStr, body)
	if err != nil {
		return err
	}
	_, err = c.inner.Do(req, out)
	return err
}

// CommentPR adds a comment to a pull request.
func (c *Client) CommentPR(ctx context.Context, repo string, number int, body string) error {
	owner, name := splitFullName(repo)
	if _, _, err := c.inner.Issues.CreateComment(ctx, owner, name, number, &github.IssueComment{
		Body: github.Ptr(body),
	}); err != nil {
		return fmt.Errorf("commenting on pull request: %w", err)
	}
	return nil
}

// ClosePR closes a pull request without merging it.
func (c *Client) ClosePR(ctx context.Context, repo string, number int) error {
	owner, name := splitFullName(repo)
	if _, _, err := c.inner.PullRequests.Edit(ctx, owner, name, number, &github.PullRequest{
		State: github.Ptr("closed"),
	}); err != nil {
		return fmt.Errorf("closing pull request: %w", err)
	}
	return nil
}

// MergePR merges a pull request.
func (c *Client) MergePR(ctx context.Context, repo string, number int, opts operations.MergeOptions) error {
	owner, name := splitFullName(repo)
	input := &github.PullRequestOptions{
		MergeMethod: githubMergeMethod(opts.MergeMethod),
	}
	if opts.Title != "" {
		input.CommitTitle = opts.Title
	}
	merged, _, err := c.inner.PullRequests.Merge(ctx, owner, name, number, opts.OptionalMessage, input)
	if err != nil {
		return fmt.Errorf("merging pull request: %w", err)
	}
	if merged != nil && !deref(merged.Merged) {
		// GitHub reports a merge that did not happen (for example a failing
		// required check) with HTTP 200 and merged=false.
		return fmt.Errorf("merge was not completed: %s", deref(merged.Message))
	}
	if opts.DeleteBranch {
		// Best effort: the merge already succeeded, so a failure to clean up
		// must not be reported as a failed merge.
		_ = c.deleteHeadBranch(ctx, owner, name, number)
	}
	return nil
}

// deleteHeadBranch removes the pull request's head branch after a merge. It is
// only meaningful for pull requests whose head lives in the base repository;
// cross-fork heads belong to the contributor and are left alone.
func (c *Client) deleteHeadBranch(ctx context.Context, owner, name string, number int) error {
	pr, _, err := c.inner.PullRequests.Get(ctx, owner, name, number)
	if err != nil {
		return err
	}
	branch := deref(pr.Head.Ref)
	if branch == "" || deref(pr.Head.Repo.FullName) != owner+"/"+name {
		return nil
	}
	_, err = c.inner.Git.DeleteRef(ctx, owner, name, "heads/"+branch)
	return err
}

// githubMergeMethod maps the unified merge method onto GitHub's vocabulary.
func githubMergeMethod(m string) string {
	switch m {
	case "squash":
		return "squash"
	case "rebase":
		return "rebase"
	default:
		return "merge"
	}
}

// SetPRReady toggles a pull request between draft and ready for review.
func (c *Client) SetPRReady(ctx context.Context, repo string, number int, ready bool) error {
	owner, name := splitFullName(repo)
	if ready {
		return c.markReadyForReview(ctx, owner, name, number)
	}
	return c.convertToDraft(ctx, owner, name, number)
}

// PRDiff returns the unified diff of a pull request.
func (c *Client) PRDiff(ctx context.Context, repo string, number int) (string, error) {
	owner, name := splitFullName(repo)
	var b strings.Builder
	// GitHub paginates large diffs, so the pages are concatenated.
	page := 1
	for {
		chunk, next, err := c.rawDiffPage(ctx, owner, name, number, page)
		if err != nil {
			return "", fmt.Errorf("fetching pull request diff: %w", err)
		}
		b.WriteString(chunk)
		if next == 0 {
			break
		}
		page = next
	}
	return b.String(), nil
}

// rawDiffPage fetches one page of a pull request diff as text. The diff
// endpoint returns a patch rather than JSON, so it is requested with the
// explicit diff media type.
func (c *Client) rawDiffPage(ctx context.Context, owner, name string, number, page int) (string, int, error) {
	u := fmt.Sprintf("repos/%s/%s/pulls/%d", owner, name, number)
	req, err := c.inner.NewRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3.diff")
	q := req.URL.Query()
	q.Set("per_page", "100")
	if page > 1 {
		q.Set("page", fmt.Sprint(page))
	}
	req.URL.RawQuery = q.Encode()

	var body strings.Builder
	resp, err := c.inner.Do(req, &body)
	if err != nil {
		return "", 0, err
	}
	return body.String(), resp.NextPage, nil
}

// FetchSpec returns how to fetch a pull request head locally.
//
// GitHub publishes every pull request head under refs/pull/N/head, including
// for forks, so the head commit can be fetched without the fork's URL or a
// collaborator token on the fork.
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
		LocalBranch: defaultBranchName(pr),
		HeadSHA:     pr.HeadSHA,
	}, nil
}

// GetIssue returns a single issue in full.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (operations.IssueDetail, error) {
	owner, name := splitFullName(repo)
	is, _, err := c.inner.Issues.Get(ctx, owner, name, number)
	if err != nil {
		return operations.IssueDetail{}, fmt.Errorf("getting issue: %w", err)
	}
	return c.issueDetail(repo, is), nil
}

// issueDetail converts an API issue into the unified detail type.
func (c *Client) issueDetail(repo string, is *github.Issue) operations.IssueDetail {
	d := operations.IssueDetail{
		Forge:    c.forgeLabelOrDefault(),
		Instance: c.Name(),
		Repo:     repo,
		Number:   int(deref(is.Number)),
		Title:    deref(is.Title),
		State:    deref(is.State),
		URL:      deref(is.HTMLURL),
		Body:     deref(is.Body),
	}
	if is.User != nil {
		d.Author = deref(is.User.Login)
	}
	for _, l := range is.Labels {
		d.Labels = append(d.Labels, deref(l.Name))
	}
	for _, a := range is.Assignees {
		d.Assignees = append(d.Assignees, deref(a.Login))
	}
	if is.CreatedAt != nil {
		d.CreatedAt = is.CreatedAt.Time
	}
	if is.UpdatedAt != nil {
		d.UpdatedAt = is.UpdatedAt.Time
	}
	return d
}

// CreateIssue opens a new issue.
func (c *Client) CreateIssue(ctx context.Context, in operations.IssueInput) (operations.IssueDetail, error) {
	owner, name := splitFullName(in.Repo)
	is, _, err := c.inner.Issues.Create(ctx, owner, name, &github.IssueRequest{
		Title: github.Ptr(in.Title),
		Body:  github.Ptr(in.Body),
	})
	if err != nil {
		return operations.IssueDetail{}, fmt.Errorf("creating issue: %w", err)
	}
	// Labels are applied separately because the create call does not accept
	// them alongside the body in every API version.
	if len(in.Labels) > 0 {
		if _, _, err := c.inner.Issues.AddLabelsToIssue(ctx, owner, name, is.GetNumber(), in.Labels); err != nil {
			return operations.IssueDetail{}, fmt.Errorf("labelling issue: %w", err)
		}
	}
	return c.issueDetail(in.Repo, is), nil
}

// CommentIssue adds a comment to an issue.
func (c *Client) CommentIssue(ctx context.Context, repo string, number int, body string) error {
	owner, name := splitFullName(repo)
	if _, _, err := c.inner.Issues.CreateComment(ctx, owner, name, number, &github.IssueComment{
		Body: github.Ptr(body),
	}); err != nil {
		return fmt.Errorf("commenting on issue: %w", err)
	}
	return nil
}

// CloseIssue closes an issue.
func (c *Client) CloseIssue(ctx context.Context, repo string, number int) error {
	owner, name := splitFullName(repo)
	if _, _, err := c.inner.Issues.Edit(ctx, owner, name, number, &github.IssueRequest{
		State: github.Ptr("closed"),
	}); err != nil {
		return fmt.Errorf("closing issue: %w", err)
	}
	return nil
}

// defaultBranchName builds the local branch name for a checked-out pull
// request, following the gh convention of prefixing with the head owner so the
// branch is recognisable when several contributors' branches are checked out.
func defaultBranchName(pr operations.PRDetail) string {
	owner := pr.HeadOwner
	if owner == "" {
		owner = pr.Author
	}
	if owner == "" {
		return pr.HeadBranch
	}
	return owner + "-" + pr.HeadBranch
}
