package hut

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pratyay360/forgit/operations"
)

// GetPR returns a single patchset in full.
//
// SourceHut's pull request analogue is a patchset on a mailing list. Patchsets
// are not strictly per-repository: a single patchset may carry changes for
// several repositories, so repo is the mailing list name here.
func (c *Client) GetPR(ctx context.Context, repo string, number int) (operations.PRDetail, error) {
	username, err := c.meUsername(ctx)
	if err != nil {
		return operations.PRDetail{}, err
	}
	var resp struct {
		Me struct {
			List struct {
				Patches struct {
					Results []struct {
						ID      int64     `json:"id"`
						Subject string    `json:"subject"`
						Status  string    `json:"status"`
						Details string    `json:"details"`
						Author  string    `json:"author"`
						URL     string    `json:"url"`
						Created time.Time `json:"created"`
					} `json:"results"`
				} `json:"patches"`
			} `json:"list"`
		} `json:"me"`
	}
	q := `query($name: String!) {
  me {
    list(name: $name) {
      patches {
        results {
          id subject status details author url created
        }
      }
    }
  }
}`
	if err := c.query(ctx, listsAPIBase, gqlRequest{
		Query:     q,
		Variables: map[string]any{"name": repo},
	}, &resp); err != nil {
		return operations.PRDetail{}, fmt.Errorf("getting patchset: %w", err)
	}
	for _, p := range resp.Me.List.Patches.Results {
		if int(p.ID) != number {
			continue
		}
		url := p.URL
		if url == "" {
			url = fmt.Sprintf("https://lists.sr.ht/~%s/%s/patches/%d", username, repo, number)
		}
		return operations.PRDetail{
			Forge:     c.forgeLabelOrDefault(),
			Instance:  c.Name(),
			Repo:      repo,
			Number:    number,
			Title:     p.Subject,
			State:     strings.ToLower(p.Status),
			URL:       url,
			Body:      p.Details,
			Author:    stripTilde(p.Author),
			CreatedAt: p.Created,
		}, nil
	}
	return operations.PRDetail{}, fmt.Errorf("patchset %d not found in list %s", number, repo)
}

// CreatePR opens a patchset.
//
// The head branch and commit cannot be set through the API: SourceHut accepts
// patches by email or over the git push-email interface, so the caller supplies
// the pushed state and forgit records the resulting patchset.
func (c *Client) CreatePR(ctx context.Context, in operations.PRInput) (operations.PRDetail, error) {
	return operations.PRDetail{}, fmt.Errorf("%w: sourcehut patchsets are created by sending patches to the list (git push-email), not through the API; see https://git.sr.ht/~%s/%s", operations.ErrNotSupported, "you", in.Repo)
}

// CommentPR adds a comment to a patchset.
//
// lists.sr.ht threads comments by thread id rather than by patchset number, so
// the patchset's thread is resolved first.
func (c *Client) CommentPR(ctx context.Context, repo string, number int, body string) error {
	thread, err := c.patchsetThread(ctx, repo, number)
	if err != nil {
		return err
	}
	var resp struct {
		AddComment struct {
			ID int64 `json:"id"`
		} `json:"addComment"`
	}
	q := `mutation($input: AddCommentInput!) {
  addComment(input: $input) { id }
}`
	if err := c.query(ctx, listsAPIBase, gqlRequest{
		Query:     q,
		Variables: map[string]any{"input": map[string]any{"thread": thread, "message": body}},
	}, &resp); err != nil {
		return fmt.Errorf("commenting on patchset: %w", err)
	}
	return nil
}

// patchsetThread resolves a patchset's comment thread id.
func (c *Client) patchsetThread(ctx context.Context, repo string, number int) (int64, error) {
	var resp struct {
		Me struct {
			List struct {
				Patches struct {
					Results []struct {
						ID            int64 `json:"id"`
						CommentThread int64 `json:"commentThread"`
					} `json:"results"`
				} `json:"patches"`
			} `json:"list"`
		} `json:"me"`
	}
	q := `query($name: String!) {
  me {
    list(name: $name) {
      patches {
        results { id commentThread }
      }
    }
  }
}`
	if err := c.query(ctx, listsAPIBase, gqlRequest{
		Query:     q,
		Variables: map[string]any{"name": repo},
	}, &resp); err != nil {
		return 0, fmt.Errorf("resolving patchset thread: %w", err)
	}
	for _, p := range resp.Me.List.Patches.Results {
		if int(p.ID) == number {
			return p.CommentThread, nil
		}
	}
	return 0, fmt.Errorf("patchset %d not found in list %s", number, repo)
}

// ClosePR closes a patchset.
//
// lists.sr.ht has no patchset state change: a patchset's status is driven by
// reviewer actions, so the patchset is deleted instead, which is the only way
// to withdraw one from review.
func (c *Client) ClosePR(ctx context.Context, repo string, number int) error {
	return fmt.Errorf("%w: sourcehut has no way to close a patchset; delete it from the web interface, or pass --force semantics by deleting the patchset directly", operations.ErrNotSupported)
}

// MergePR merges a patchset.
//
// SourceHut has no merge button: patches are applied by a maintainer with
// git-am or the series-merge helper on the repository itself.
func (c *Client) MergePR(ctx context.Context, repo string, number int, opts operations.MergeOptions) error {
	return fmt.Errorf("%w: sourcehut has no merge API; patches are applied by maintainers using git-am or series-merge", operations.ErrNotSupported)
}

// PRDiff returns the diff of a patchset.
//
// lists.sr.ht does not expose a diff through GraphQL; each patchset revision is
// an ordinary git patch whose series is fetched from git.sr.ht, so the unified
// diff is produced locally from the fetched commits instead of the API.
func (c *Client) PRDiff(ctx context.Context, repo string, number int) (string, error) {
	return "", fmt.Errorf(
		"%w: sourcehut does not expose a patchset diff through the API; fetch the series from git.sr.ht and diff it locally, or view the patchset at https://lists.sr.ht/~%s/%s",
		operations.ErrNotSupported, "you", repo)
}

// FetchSpec returns how to fetch a patchset's branch locally.
//
// SourceHut has no pull request ref namespace. A patchset is a mail series
// whose commits a maintainer applies to a branch, so there is no single ref
// that stands in for the review. The caller must name the branch to fetch.
func (c *Client) FetchSpec(ctx context.Context, repo string, number int) (operations.FetchSpec, error) {
	if _, err := c.GetPR(ctx, repo, number); err != nil {
		return operations.FetchSpec{}, err
	}
	return operations.FetchSpec{}, fmt.Errorf(
		"%w: sourcehut patchsets are mail series rather than review refs, so there is no ref to fetch; fetch the branch the series targets (git fetch origin <branch>) and apply it with git am",
		operations.ErrNotSupported)
}

// GetIssue returns a single ticket in full.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (operations.IssueDetail, error) {
	t, err := c.ticket(ctx, repo, number)
	if err != nil {
		return operations.IssueDetail{}, err
	}
	return c.issueDetail(repo, t), nil
}

// ticketDetail mirrors the GraphQL Ticket object of todo.sr.ht, including the
// fields forgit's unified issue detail exposes.
type ticketDetail struct {
	ID      int64     `json:"id"`
	Title   string    `json:"subject"`
	Status  string    `json:"status"`
	Body    string    `json:"body"`
	Author  string    `json:"author"`
	URL     string    `json:"url"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Labels  []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Username string `json:"username"`
	} `json:"assignees"`
}

// ticketQuery fetches a tracker's tickets with the fields forgit needs.
const ticketQuery = `query($name: String!) {
  me {
    tracker(name: $name) {
      tickets {
        results {
          id subject status body author url created updated
          labels { name }
          assignees { username }
        }
      }
    }
  }
}`

// ticket fetches one ticket from a tracker by id.
func (c *Client) ticket(ctx context.Context, trackerName string, number int) (*ticketDetail, error) {
	var resp struct {
		Me struct {
			Tracker struct {
				Tickets struct {
					Results []ticketDetail `json:"results"`
				} `json:"tickets"`
			} `json:"tracker"`
		} `json:"me"`
	}
	if err := c.query(ctx, todoAPIBase, gqlRequest{
		Query:     ticketQuery,
		Variables: map[string]any{"name": trackerName},
	}, &resp); err != nil {
		return nil, fmt.Errorf("getting ticket: %w", err)
	}
	for i := range resp.Me.Tracker.Tickets.Results {
		if int(resp.Me.Tracker.Tickets.Results[i].ID) == number {
			return &resp.Me.Tracker.Tickets.Results[i], nil
		}
	}
	return nil, fmt.Errorf("ticket %d not found in tracker %s", number, trackerName)
}

// issueDetail converts a ticket into the unified issue detail type.
func (c *Client) issueDetail(repo string, t *ticketDetail) operations.IssueDetail {
	d := operations.IssueDetail{
		Forge:     c.forgeLabelOrDefault(),
		Instance:  c.Name(),
		Repo:      repo,
		Number:    int(t.ID),
		Title:     t.Title,
		State:     strings.ToLower(t.Status),
		URL:       t.URL,
		Body:      t.Body,
		Author:    stripTilde(t.Author),
		CreatedAt: t.Created,
		UpdatedAt: t.Updated,
	}
	for _, l := range t.Labels {
		d.Labels = append(d.Labels, l.Name)
	}
	for _, a := range t.Assignees {
		d.Assignees = append(d.Assignees, a.Username)
	}
	return d
}

// CreateIssue opens a new ticket on a tracker.
func (c *Client) CreateIssue(ctx context.Context, in operations.IssueInput) (operations.IssueDetail, error) {
	var resp struct {
		CreateIssue struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"createIssue"`
	}
	q := `mutation($input: CreateIssueInput!) {
  createIssue(input: $input) { id title }
}`
	if err := c.query(ctx, todoAPIBase, gqlRequest{
		Query: q,
		Variables: map[string]any{"input": map[string]any{
			"tracker": in.Repo,
			"title":   in.Title,
			"message": in.Body,
		}},
	}, &resp); err != nil {
		return operations.IssueDetail{}, fmt.Errorf("creating ticket: %w", err)
	}
	if resp.CreateIssue.ID == 0 {
		return operations.IssueDetail{}, fmt.Errorf("creating ticket: the forge did not return a ticket id")
	}
	return operations.IssueDetail{
		Forge:    c.forgeLabelOrDefault(),
		Instance: c.Name(),
		Repo:     in.Repo,
		Number:   int(resp.CreateIssue.ID),
		Title:    in.Title,
		State:    "reported",
		Body:     in.Body,
	}, nil
}

// CommentIssue adds a comment to a ticket.
func (c *Client) CommentIssue(ctx context.Context, repo string, number int, body string) error {
	t, err := c.ticket(ctx, repo, number)
	if err != nil {
		return err
	}
	var resp struct {
		AddComment struct {
			ID int64 `json:"id"`
		} `json:"addComment"`
	}
	q := `mutation($input: AddCommentInput!) {
  addComment(input: $input) { id }
}`
	thread := t.ID
	if err := c.query(ctx, todoAPIBase, gqlRequest{
		Query:     q,
		Variables: map[string]any{"input": map[string]any{"thread": thread, "message": body}},
	}, &resp); err != nil {
		return fmt.Errorf("commenting on ticket: %w", err)
	}
	return nil
}

// CloseIssue closes a ticket by moving it to the closed status.
func (c *Client) CloseIssue(ctx context.Context, repo string, number int) error {
	t, err := c.ticket(ctx, repo, number)
	if err != nil {
		return err
	}
	var resp struct {
		UpdateTicket struct {
			ID int64 `json:"id"`
		} `json:"updateTicket"`
	}
	q := `mutation($input: UpdateTicketInput!) {
  updateTicket(input: $input) { id }
}`
	if err := c.query(ctx, todoAPIBase, gqlRequest{
		Query: q,
		Variables: map[string]any{"input": map[string]any{
			"id":     t.ID,
			"status": "CLOSED",
		}},
	}, &resp); err != nil {
		return fmt.Errorf("closing ticket: %w", err)
	}
	return nil
}
