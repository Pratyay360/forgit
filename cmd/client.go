package cmd

import (
	"context"
	"fmt"

	"github.com/pratyay360/forgit/operations"
	"github.com/pratyay360/forgit/operations/bitbucket"
	"github.com/pratyay360/forgit/operations/forgejo"
	"github.com/pratyay360/forgit/operations/github"
	"github.com/pratyay360/forgit/operations/gitlab"
	"github.com/pratyay360/forgit/operations/hut"
)

type forgeClient interface {
	Name() string
	ListRepos(context.Context) ([]operations.Repo, error)
	CreateRepo(context.Context, operations.RepoInput) (operations.Repo, error)
	RenameRepo(context.Context, string, string) (operations.Repo, error)
	DeleteRepo(context.Context, string) error
	SetVisibility(context.Context, string, bool) (operations.Repo, error)
	ListIssues(context.Context) ([]operations.Issue, error)
	ListPRs(context.Context) ([]operations.PR, error)
	ListRuns(context.Context, string) ([]operations.Run, error)
	ListWorkflows(context.Context, string) ([]operations.Workflow, error)
	ListProjects(context.Context) ([]operations.Project, error)
}

// prReader reads individual pull requests.
type prReader interface {
	GetPR(ctx context.Context, repo string, number int) (operations.PRDetail, error)
}

// prWriter performs the mutating pull request operations.
type prWriter interface {
	CreatePR(ctx context.Context, in operations.PRInput) (operations.PRDetail, error)
	CommentPR(ctx context.Context, repo string, number int, body string) error
	ClosePR(ctx context.Context, repo string, number int) error
	MergePR(ctx context.Context, repo string, number int, opts operations.MergeOptions) error
}

type prChecker interface {
	SetPRReady(ctx context.Context, repo string, number int, ready bool) error
}

// prDiffer returns a pull request's patch text.
type prDiffer interface {
	PRDiff(ctx context.Context, repo string, number int) (string, error)
}

type prFetcher interface {
	FetchSpec(ctx context.Context, repo string, number int) (operations.FetchSpec, error)
}

// issueReader reads individual issues.
type issueReader interface {
	GetIssue(ctx context.Context, repo string, number int) (operations.IssueDetail, error)
}

// issueWriter performs the mutating issue operations.
type issueWriter interface {
	CreateIssue(ctx context.Context, in operations.IssueInput) (operations.IssueDetail, error)
	CommentIssue(ctx context.Context, repo string, number int, body string) error
	CloseIssue(ctx context.Context, repo string, number int) error
}

// as asserts a client to a capability interface, reporting whether the forge
// supports it. Callers turn a false result into an explanatory note rather
// than a hard failure, which is what keeps commands uniform across forges.
func as[T any](c forgeClient) (T, bool) {
	v, ok := c.(T)
	return v, ok
}

// errNoCapability renders the standard message for a forge that lacks a
// capability.
func errNoCapability(forge, feature string) error {
	return fmt.Errorf("%w: %s does not support %s", operations.ErrNotSupported, forge, feature)
}

// clients builds one client per configured instance (including multiple
// instances of the same forge type and user-named aliases of built-in
// drivers). An alias uses inst.Driver to pick the underlying client and
// inst.Type as the public label that gets stamped on every returned
// resource.
func clients(cfg operations.Config) ([]forgeClient, error) {
	var cs []forgeClient
	for _, inst := range cfg.All() {
		driver := inst.Type
		if inst.Driver != "" {
			driver = inst.Driver
		}
		switch driver {
		case "github":
			c, err := github.New(operations.GitHubConfig{Token: inst.Token})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			c.SetForgeLabel(inst.Type)
			cs = append(cs, c)
		case "gitlab":
			c, err := gitlab.New(operations.GitLabConfig{Token: inst.Token, URL: inst.URL})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			c.SetForgeLabel(inst.Type)
			cs = append(cs, c)
		case "forgejo":
			c, err := forgejo.New(operations.ForgejoConfig{Token: inst.Token, URL: inst.URL})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			c.SetForgeLabel(inst.Type)
			cs = append(cs, c)
		case "sourcehut":
			c, err := hut.New(operations.SourceHutConfig{Token: inst.Token})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			c.SetForgeLabel(inst.Type)
			cs = append(cs, c)
		case "bitbucket":
			c, err := bitbucket.New(operations.BitbucketConfig{Token: inst.Token, Username: inst.Username, Workspace: inst.Workspace})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			c.SetForgeLabel(inst.Type)
			cs = append(cs, c)
		default:
			return nil, fmt.Errorf("unknown forge type %q in instance %q", inst.Type, inst.Name)
		}
	}
	return cs, nil
}
