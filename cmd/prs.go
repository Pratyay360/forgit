package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/pratyay360/forge/operations"
	"github.com/spf13/cobra"
)

var prsCmd = &cobra.Command{
	Use:   "prs",
	Short: "List open pull requests across all configured forges",
	Long: `List open pull requests across all configured forges.

For forges without a global pull-request API (GitHub, Forgejo, Bitbucket)
the ten most recently listed repositories are scanned.`,
	RunE: runPRs,
}

func init() {
	rootCmd.AddCommand(prsCmd)
}

func runPRs(cmd *cobra.Command, args []string) error {
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

	var all []operations.PR
	failed, ok := 0, 0
	for _, c := range cs {
		prs, err := c.ListPRs(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			failed++
			continue
		}
		all = append(all, prs...)
		ok++
	}
	if len(all) == 0 && failed == len(cs) {
		return fmt.Errorf("no pull requests fetched: every forge failed")
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
	for _, pr := range all {
		rows = append(rows, []string{
			pr.Instance,
			pr.Repo,
			strconv.Itoa(pr.Number),
			pr.State,
			pr.Title,
			pr.URL,
		})
	}
	if err := writeTable(os.Stdout, []string{"FORGE", "REPOSITORY", "#", "STATE", "TITLE", "URL"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d pull requests from %d forges\n", len(all), ok)
	return nil
}
