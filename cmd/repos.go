package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

var reposCmd = &cobra.Command{
	Use:   "repos",
	Short: "List repositories across all configured forges",
	RunE:  runRepos,
}

func init() {
	rootCmd.AddCommand(reposCmd)
}

func runRepos(cmd *cobra.Command, args []string) error {
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

	var all []operations.Repo
	failed, ok := 0, 0
	for _, c := range cs {
		repos, err := c.ListRepos(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			failed++
			continue
		}
		all = append(all, repos...)
		ok++
	}
	if len(all) == 0 && failed == len(cs) {
		return fmt.Errorf("no repositories fetched: every forge failed")
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Instance != all[j].Instance {
			return all[i].Instance < all[j].Instance
		}
		return all[i].FullName < all[j].FullName
	})

	rows := make([][]string, 0, len(all))
	for _, r := range all {
		name := r.FullName
		if r.Private {
			name += " (private)"
		}
		rows = append(rows, []string{r.Instance, name, r.URL})
	}
	if err := writeTable(os.Stdout, []string{"FORGE", "REPOSITORY", "URL"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d repositories from %d forges\n", len(all), ok)
	return nil
}
