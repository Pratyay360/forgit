package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/pratyay360/forge/operations"
	"github.com/spf13/cobra"
)

var runsCmd = &cobra.Command{
	Use:   "runs [REPO]",
	Short: "List CI runs across all configured forges",
	Long: `List CI runs across all configured forges.

With a REPO argument (owner/name) only runs for that repository are shown on
every configured forge. Without it, runs for each forge's repositories are
listed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRuns,
}

func init() {
	rootCmd.AddCommand(runsCmd)
	runsCmd.Flags().String("instance", "", "target a specific configured instance")
	runsCmd.Flags().String("status", "", "only show runs with this status (e.g. success, failure, running)")
}

func runRuns(cmd *cobra.Command, args []string) error {
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

	statusFilter := flagString(cmd, "status")
	repoArg := ""
	if len(args) == 1 {
		repoArg = args[0]
	}

	var all []operations.Run
	failed := 0
	for _, c := range cs {
		runs, err := listRunsFor(ctx, c, repoArg)
		if err != nil {
			if errors.Is(err, operations.ErrNotSupported) {
				fmt.Fprintf(os.Stderr, "note: %s: %v\n", c.Name(), err)
				continue
			}
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			failed++
			continue
		}
		for _, r := range runs {
			if statusFilter == "" || r.Status == statusFilter {
				all = append(all, r)
			}
		}
	}
	if len(all) == 0 && failed == len(cs) {
		return fmt.Errorf("no runs fetched: every forge failed")
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Instance != all[j].Instance {
			return all[i].Instance < all[j].Instance
		}
		if all[i].Repo != all[j].Repo {
			return all[i].Repo < all[j].Repo
		}
		return all[i].ID > all[j].ID // newest first
	})

	rows := make([][]string, 0, len(all))
	for _, r := range all {
		id := strconv.FormatInt(r.ID, 10)
		if r.Name != "" {
			id += " " + r.Name
		}
		rows = append(rows, []string{
			r.Instance,
			r.Repo,
			id,
			r.Status,
			r.Branch,
			r.URL,
		})
	}
	if err := writeTable(os.Stdout, []string{"FORGE", "REPOSITORY", "RUN", "STATUS", "BRANCH", "URL"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d runs\n", len(all))
	return nil
}

// listRunsFor lists runs for a single repository when repoArg is set, or for
// every repository across the forge otherwise.
func listRunsFor(ctx context.Context, c forgeClient, repoArg string) ([]operations.Run, error) {
	if repoArg != "" {
		return c.ListRuns(ctx, repoArg)
	}
	repos, err := c.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	var all []operations.Run
	for _, r := range repos {
		runs, err := c.ListRuns(ctx, r.FullName)
		if err != nil {
			if errors.Is(err, operations.ErrNotSupported) {
				return nil, err
			}
			continue
		}
		all = append(all, runs...)
	}
	return all, nil
}
