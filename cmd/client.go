package cmd

import (
	"context"
	"fmt"

	"github.com/pratyay360/forge/v1/operations"
	"github.com/pratyay360/forge/v1/operations/bitbucket"
	"github.com/pratyay360/forge/v1/operations/forgejo"
	"github.com/pratyay360/forge/v1/operations/github"
	"github.com/pratyay360/forge/v1/operations/gitlab"
	"github.com/pratyay360/forge/v1/operations/hut"
)

// forgeClient is implemented by every forge-specific client. Every operation
// is part of the unified surface so commands stay uniform across forges;
// a forge that lacks a concept returns operations.ErrNotSupported.
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

// clients builds one client per configured instance (including multiple
// instances of the same forge type).
func clients(cfg operations.Config) ([]forgeClient, error) {
	var cs []forgeClient
	for _, inst := range cfg.All() {
		switch inst.Type {
		case "github":
			c, err := github.New(operations.GitHubConfig{Token: inst.Token})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			cs = append(cs, c)
		case "gitlab":
			c, err := gitlab.New(operations.GitLabConfig{Token: inst.Token, URL: inst.URL})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			cs = append(cs, c)
		case "forgejo":
			c, err := forgejo.New(operations.ForgejoConfig{Token: inst.Token, URL: inst.URL})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			cs = append(cs, c)
		case "sourcehut":
			c, err := hut.New(operations.SourceHutConfig{Token: inst.Token})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			cs = append(cs, c)
		case "bitbucket":
			c, err := bitbucket.New(operations.BitbucketConfig{Token: inst.Token, Username: inst.Username})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.Name, err)
			}
			c.SetInstance(inst.Name)
			cs = append(cs, c)
		default:
			return nil, fmt.Errorf("unknown forge type %q in instance %q", inst.Type, inst.Name)
		}
	}
	return cs, nil
}
