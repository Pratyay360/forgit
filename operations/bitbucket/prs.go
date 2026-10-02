package bitbucket

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pratyay360/forgit/operations"
)

// prDetailItem is the subset of the pull request payload forgit uses.
type prDetailItem struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Summary struct {
		Raw string `json:"raw"`
	} `json:"summary"`
	Author struct {
		Nickname string `json:"nickname"`
	} `json:"author"`
	Source struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Commit struct {
			Hash string `json:"hash"`
		} `json:"commit"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	} `json:"source"`
	Destination struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"destination"`
	CreatedOn time.Time `json:"created_on"`
	UpdatedOn time.Time `json:"updated_on"`
	Links     struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
		Diff struct {
			Href string `json:"href"`
		} `json:"diff"`
	} `json:"links"`
}

// GetPR returns a single pull request in full.
func (c *Client) GetPR(ctx context.Context, repo string, number int) (operations.PRDetail, error) {
	var pr prDetailItem
	u := fmt.Sprintf("%s/repositories/%s/pullrequests/%d", apiBase, fullNamePath(repo), number)
	if err := c.get(ctx, u, &pr); err != nil {
		return operations.PRDetail{}, fmt.Errorf("getting pull request: %w", err)
	}
	return c.prDetail(repo, &pr), nil
}

// prDetail converts an API pull request into the unified detail type.
func (c *Client) prDetail(repo string, pr *prDetailItem) operations.PRDetail {
	state := strings.ToLower(pr.State)
	return operations.PRDetail{
		Forge:        "bitbucket",
		Instance:     c.Name(),
		Repo:         repo,
		Number:       int(pr.ID),
		Title:        pr.Title,
		State:        state,
		URL:          pr.Links.HTML.Href,
		Body:         pr.Summary.Raw,
		Merged:       state == "merged",
		Author:       pr.Author.Nickname,
		HeadBranch:   pr.Source.Branch.Name,
		HeadSHA:      pr.Source.Commit.Hash,
		HeadOwner:    workspaceOf(pr.Source.Repository.FullName),
		HeadRepoName: repoNameOf(pr.Source.Repository.FullName),
		BaseBranch:   pr.Destination.Branch.Name,
		CreatedAt:    pr.CreatedOn,
		UpdatedAt:    pr.UpdatedOn,
	}
}

// workspaceOf returns the workspace part of a workspace/repo slug.
func workspaceOf(fullName string) string {
	if i := strings.Index(fullName, "/"); i > 0 {
		return fullName[:i]
	}
	return ""
}

// repoNameOf returns the repository part of a workspace/repo slug.
func repoNameOf(fullName string) string {
	if i := strings.Index(fullName, "/"); i > 0 {
		return fullName[i+1:]
	}
	return ""
}

// CreatePR opens a pull request.
func (c *Client) CreatePR(ctx context.Context, in operations.PRInput) (operations.PRDetail, error) {
	payload := map[string]any{
		"title": in.Title,
		"source": map[string]any{
			"branch": map[string]any{"name": in.Head},
		},
	}
	if in.Body != "" {
		payload["description"] = in.Body
	}
	if in.Base != "" {
		payload["destination"] = map[string]any{
			"branch": map[string]any{"name": in.Base},
		}
	}

	var pr prDetailItem
	u := fmt.Sprintf("%s/repositories/%s/pullrequests", apiBase, fullNamePath(in.Repo))
	if err := c.post(ctx, u, payload, &pr); err != nil {
		return operations.PRDetail{}, fmt.Errorf("creating pull request: %w", err)
	}
	return c.prDetail(in.Repo, &pr), nil
}

// CommentPR adds a comment to a pull request.
//
// Bitbucket has no issue tracker, but pull request comments are available on the
// same endpoints the retired tracker used, so comments still work.
func (c *Client) CommentPR(ctx context.Context, repo string, number int, body string) error {
	u := fmt.Sprintf("%s/repositories/%s/pullrequests/%d/comments", apiBase, fullNamePath(repo), number)
	if err := c.post(ctx, u, map[string]any{"content": map[string]any{"raw": body}}, nil); err != nil {
		return fmt.Errorf("commenting on pull request: %w", err)
	}
	return nil
}

// ClosePR declines a pull request, which is Bitbucket's equivalent of closing.
func (c *Client) ClosePR(ctx context.Context, repo string, number int) error {
	u := fmt.Sprintf("%s/repositories/%s/pullrequests/%d/decline", apiBase, fullNamePath(repo), number)
	if err := c.post(ctx, u, nil, nil); err != nil {
		return fmt.Errorf("closing pull request: %w", err)
	}
	return nil
}

// MergePR merges a pull request.
//
// Bitbucket has no draft concept and no readiness toggle, so the draft-related
// operations are deliberately absent: this client satisfies prWriter but not
// prChecker, and the command layer reports that as a note.
func (c *Client) MergePR(ctx context.Context, repo string, number int, opts operations.MergeOptions) error {
	payload := map[string]any{
		"close_source_branch": opts.DeleteBranch,
	}
	msg := opts.Title
	if msg == "" {
		msg = opts.OptionalMessage
	}
	if msg != "" {
		payload["message"] = msg
	}
	if opts.MergeMethod == "squash" {
		payload["merge_strategy"] = "squash"
	}

	u := fmt.Sprintf("%s/repositories/%s/pullrequests/%d/merge", apiBase, fullNamePath(repo), number)
	if err := c.post(ctx, u, payload, nil); err != nil {
		return fmt.Errorf("merging pull request: %w", err)
	}
	return nil
}

// PRDiff returns the unified diff of a pull request.
func (c *Client) PRDiff(ctx context.Context, repo string, number int) (string, error) {
	var pr prDetailItem
	u := fmt.Sprintf("%s/repositories/%s/pullrequests/%d", apiBase, fullNamePath(repo), number)
	if err := c.get(ctx, u, &pr); err != nil {
		return "", fmt.Errorf("fetching pull request: %w", err)
	}
	diffURL := pr.Links.Diff.Href
	if diffURL == "" {
		return "", fmt.Errorf("pull request %d has no diff available", number)
	}
	diff, _, err := c.rawGet(ctx, diffURL, "text/plain")
	if err != nil {
		return "", fmt.Errorf("fetching pull request diff: %w", err)
	}
	return diff, nil
}

// FetchSpec returns how to fetch a pull request head locally.
//
// Bitbucket publishes no pull request ref namespace, so the head branch itself
// is fetched. That only works when the head lives in the base repository: for a
// pull request opened from a fork the branch exists solely on the fork, so the
// spec points at the fork's clone URL under a distinct remote name and the
// caller adds that remote before fetching.
func (c *Client) FetchSpec(ctx context.Context, repo string, number int) (operations.FetchSpec, error) {
	pr, err := c.GetPR(ctx, repo, number)
	if err != nil {
		return operations.FetchSpec{}, err
	}
	if pr.HeadBranch == "" {
		return operations.FetchSpec{}, fmt.Errorf("pull request %d has no head branch", number)
	}

	ref := fmt.Sprintf("refs/heads/%s", pr.HeadBranch)
	spec := operations.FetchSpec{
		Remote:      "origin",
		Refspecs:    []string{fmt.Sprintf("+%s:%s", ref, ref)},
		LocalBranch: branchName(pr),
		HeadSHA:     pr.HeadSHA,
	}

	baseWorkspace := workspaceOf(repo)
	if pr.HeadOwner != "" && pr.HeadOwner != baseWorkspace {
		// Cross-workspace pull request: point the fetch at the fork so the
		// branch actually resolves.
		forkRepo := pr.HeadRepoName
		if forkRepo == "" {
			// Without the fork's slug the only safe answer is to ask the caller
			// to add the fork as a remote.
			return operations.FetchSpec{}, fmt.Errorf(
				"pull request %d comes from the %s workspace; add that fork as a git remote (for example: git remote add fork https://bitbucket.org/%s/<repo>.git) and pass --remote fork",
				number, pr.HeadOwner, pr.HeadOwner)
		}
		forkURL, err := c.forkRemoteURL(ctx, pr.HeadOwner, forkRepo)
		if err != nil {
			return operations.FetchSpec{}, err
		}
		spec.Remote = ""
		spec.ForkURL = forkURL
	}
	return spec, nil
}

// branchName builds the local branch name, prefixing the pull request author so
// branches from several contributors stay distinguishable.
func branchName(pr operations.PRDetail) string {
	owner := pr.HeadOwner
	if owner == "" {
		owner = pr.Author
	}
	if owner == "" {
		return pr.HeadBranch
	}
	return owner + "-" + pr.HeadBranch
}

// forkRemoteURL returns the clone URL of a fork, used when the head branch only
// exists there.
func (c *Client) forkRemoteURL(ctx context.Context, workspace, repoSlug string) (string, error) {
	var body repoItem
	u := fmt.Sprintf("%s/repositories/%s/%s", apiBase, url.PathEscape(workspace), url.PathEscape(repoSlug))
	if err := c.get(ctx, u, &body); err != nil {
		return "", err
	}
	if body.Links.HTML.Href == "" {
		return "", fmt.Errorf("no clone URL for %s/%s", workspace, repoSlug)
	}
	return body.Links.HTML.Href, nil
}
