package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

var workflowsCmd = &cobra.Command{
	Use:   "workflows [REPO]",
	Short: "List CI workflow definitions across all configured forges",
	Long: `List CI workflow definitions across all configured forges.

With a REPO argument (owner/name) only workflows for that repository are shown
on every configured forge. Without it, workflows for each forge's repositories
are listed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkflows,
}

func init() {
	rootCmd.AddCommand(workflowsCmd)
	workflowsCmd.Flags().String("instance", "", "target a specific configured instance")
}

func runWorkflows(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	cs, err := clients(cfg)
	if err != nil {
		return err
	}
	if inst := flagString(cmd, "instance"); inst != "" {
		cs = filterClients(cs, inst)
		if len(cs) == 0 {
			return fmt.Errorf("no configured instance named %q", inst)
		}
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 90*time.Second)
	defer cancel()

	repoArg := ""
	if len(args) == 1 {
		repoArg = args[0]
	}

	var all []operations.Workflow
	failed := 0
	for _, c := range cs {
		workflows, err := listWorkflowsFor(ctx, c, repoArg)
		if err != nil {
			if errors.Is(err, operations.ErrNotSupported) {
				fmt.Fprintf(os.Stderr, "note: %s: %v\n", c.Name(), err)
				continue
			}
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			failed++
			continue
		}
		all = append(all, workflows...)
	}
	if len(all) == 0 && failed == len(cs) {
		return fmt.Errorf("no workflows fetched: every forge failed")
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Instance != all[j].Instance {
			return all[i].Instance < all[j].Instance
		}
		if all[i].Repo != all[j].Repo {
			return all[i].Repo < all[j].Repo
		}
		return all[i].Name < all[j].Name
	})

	rows := make([][]string, 0, len(all))
	for _, w := range all {
		id := strconv.FormatInt(w.ID, 10)
		rows = append(rows, []string{
			w.Instance,
			w.Repo,
			id,
			w.Name,
			w.State,
			w.URL,
		})
	}
	if err := writeTable(os.Stdout, []string{"FORGE", "REPOSITORY", "ID", "WORKFLOW", "STATE", "URL"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d workflows\n", len(all))
	return nil
}

func listWorkflowsFor(ctx context.Context, c forgeClient, repoArg string) ([]operations.Workflow, error) {
	if repoArg != "" {
		return c.ListWorkflows(ctx, repoArg)
	}
	repos, err := c.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	var all []operations.Workflow
	for _, r := range repos {
		workflows, err := c.ListWorkflows(ctx, r.FullName)
		if err != nil {
			if errors.Is(err, operations.ErrNotSupported) {
				return nil, err
			}
			continue
		}
		all = append(all, workflows...)
	}
	return all, nil
}
