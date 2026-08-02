package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/pratyay360/forge/v1/operations"
	"github.com/spf13/cobra"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List projects across all configured forges",
	Long: `List projects across all configured forges.

Projects map to the forge's own concept: GitHub project boards, GitLab
projects and Bitbucket workspace projects. Forges without a projects concept
(GitLab is supported; SourceHut and Forgejo are not) are noted.`,
	RunE: runProjects,
}

func init() {
	rootCmd.AddCommand(projectsCmd)
	projectsCmd.Flags().String("instance", "", "target a specific configured instance")
}

func runProjects(cmd *cobra.Command, args []string) error {
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
	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	var all []operations.Project
	failed := 0
	for _, c := range cs {
		projects, err := c.ListProjects(ctx)
		if err != nil {
			if errors.Is(err, operations.ErrNotSupported) {
				fmt.Fprintf(os.Stderr, "note: %s: %v\n", c.Name(), err)
				continue
			}
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			failed++
			continue
		}
		for i := range projects {
			projects[i].Instance = c.Name()
		}
		all = append(all, projects...)
	}
	if len(all) == 0 && failed == len(cs) {
		return fmt.Errorf("no projects fetched: every forge failed")
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Instance != all[j].Instance {
			return all[i].Instance < all[j].Instance
		}
		return all[i].FullName < all[j].FullName
	})

	rows := make([][]string, 0, len(all))
	for _, p := range all {
		name := p.FullName
		if p.Private {
			name += " (private)"
		}
		rows = append(rows, []string{p.Instance, name, p.URL})
	}
	if err := writeTable(os.Stdout, []string{"FORGE", "PROJECT", "URL"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d projects\n", len(all))
	return nil
}
