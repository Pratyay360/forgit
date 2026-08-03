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

var issuesCmd = &cobra.Command{
	Use:   "issues",
	Short: "List open issues across all configured forges",
	RunE:  runIssues,
}

func init() {
	rootCmd.AddCommand(issuesCmd)
}

func runIssues(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	cs, err := clients(cfg)
	if err != nil {
		return err
	}

	var all []operations.Issue
	failed, ok := 0, 0
	for _, c := range cs {
		issues, err := c.ListIssues(ctx)
		if err != nil {
			if errors.Is(err, operations.ErrIssuesUnsupported) {
				// Not a failure: the forge has no issue tracker (e.g. Bitbucket).
				fmt.Fprintf(os.Stderr, "note: %s: %v\n", c.Name(), err)
				continue
			}
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			failed++
			continue
		}
		all = append(all, issues...)
		ok++
	}
	if len(all) == 0 && failed == len(cs) {
		return fmt.Errorf("no issues fetched: every forge failed")
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Instance != all[j].Instance {
			return all[i].Instance < all[j].Instance
		}
		if all[i].Repo != all[j].Repo {
			return all[i].Repo < all[j].Repo
		}
		return all[i].Number < all[j].Number
	})

	rows := make([][]string, 0, len(all))
	for _, is := range all {
		rows = append(rows, []string{
			is.Instance,
			is.Repo,
			strconv.Itoa(is.Number),
			is.State,
			is.Title,
			is.URL,
		})
	}
	if err := writeTable(os.Stdout, []string{"FORGE", "REPOSITORY", "#", "STATE", "TITLE", "URL"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d issues from %d forges\n", len(all), ok)
	return nil
}
