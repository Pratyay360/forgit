package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

// repoCmd groups repository lifecycle operations: create, rename, delete and
// visibility. They are uniform across every configured forge.
var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage repositories (create, rename, delete, visibility)",
}

func init() {
	rootCmd.AddCommand(repoCmd)
	repoCmd.AddCommand(repoCreateCmd, repoRenameCmd, repoDeleteCmd, repoVisibilityCmd)

	repoCreateCmd.Flags().String("owner", "", "owner namespace (organization/group); defaults to the authenticated user")
	repoCreateCmd.Flags().String("description", "", "repository description")
	repoCreateCmd.Flags().Bool("private", false, "create as a private repository")
	repoCreateCmd.Flags().String("instance", "", "target a specific configured instance (default: every instance)")
	repoCreateCmd.Flags().Bool("dry-run", false, "print what would happen without calling the API")

	repoRenameCmd.Flags().String("instance", "", "target a specific configured instance")
	repoDeleteCmd.Flags().String("instance", "", "target a specific configured instance")
	repoDeleteCmd.Flags().Bool("yes", false, "skip the confirmation prompt")
	repoVisibilityCmd.Flags().String("instance", "", "target a specific configured instance")
}

var repoCreateCmd = &cobra.Command{
	Use:   "create NAME",
	Short: "Create a repository on every configured forge",
	Args:  cobra.ExactArgs(1),
	RunE:  runRepoCreate,
}

var repoRenameCmd = &cobra.Command{
	Use:   "rename REPO NEW_NAME",
	Short: "Rename a repository on every configured forge",
	Long: `Rename a repository on every configured forge.

REPO is the owner/name (e.g. alice/alpha). The command targets every
configured instance; use --instance to limit it to one.`,
	Args: cobra.ExactArgs(2),
	RunE: runRepoRename,
}

var repoDeleteCmd = &cobra.Command{
	Use:   "delete REPO",
	Short: "Delete a repository on every configured forge",
	Long: `Delete a repository on every configured forge.

REPO is the owner/name (e.g. alice/alpha). Use --yes to skip the
confirmation prompt and --instance to target one instance.`,
	Args: cobra.ExactArgs(1),
	RunE: runRepoDelete,
}

var repoVisibilityCmd = &cobra.Command{
	Use:   "visibility REPO public|private",
	Short: "Change a repository's visibility on every configured forge",
	Args:  cobra.ExactArgs(2),
	RunE:  runRepoVisibility,
}

// repoContext carries the resolved config, client list and timeout for the
// repo management commands.
func repoContext(cmd *cobra.Command) ([]forgeClient, context.Context, context.CancelFunc, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	cs, err := clients(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if inst := flagString(cmd, "instance"); inst != "" {
		cs = filterClients(cs, inst)
		if len(cs) == 0 {
			return nil, nil, nil, fmt.Errorf("no configured instance named %q", inst)
		}
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	return cs, ctx, cancel, nil
}

func filterClients(cs []forgeClient, instance string) []forgeClient {
	var out []forgeClient
	for _, c := range cs {
		if c.Name() == instance {
			out = append(out, c)
		}
	}
	return out
}

func runRepoCreate(cmd *cobra.Command, args []string) error {
	cs, ctx, cancel, err := repoContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	name := args[0]
	in := operations.RepoInput{
		Name:        name,
		Owner:       flagString(cmd, "owner"),
		Description: flagString(cmd, "description"),
		Private:     boolFlag(cmd, "private"),
	}
	dryRun := boolFlag(cmd, "dry-run")

	var created []operations.Repo
	for _, c := range cs {
		if dryRun {
			owner := in.Owner
			if owner == "" {
				owner = "you"
			}
			vis := "public"
			if in.Private {
				vis = "private"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "dry-run: %s: would create %s/%s (%s)\n", c.Name(), owner, name, vis); err != nil {
				return err
			}
			continue
		}
		repo, err := c.CreateRepo(ctx, in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			continue
		}
		created = append(created, repo)
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: created %s\n", c.Name(), repo.URL); err != nil {
			return err
		}
	}
	if !dryRun && len(created) == 0 {
		return fmt.Errorf("no repositories created: every forge failed")
	}
	return nil
}

func runRepoRename(cmd *cobra.Command, args []string) error {
	cs, ctx, cancel, err := repoContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	fullName, newName := args[0], args[1]
	var renamed []operations.Repo
	for _, c := range cs {
		repo, err := c.RenameRepo(ctx, fullName, newName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			continue
		}
		renamed = append(renamed, repo)
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: renamed %s -> %s\n", c.Name(), fullName, repo.FullName); err != nil {
			return err
		}
	}
	if len(renamed) == 0 {
		return fmt.Errorf("no repositories renamed: every forge failed")
	}
	return nil
}

func runRepoDelete(cmd *cobra.Command, args []string) error {
	if !boolFlag(cmd, "yes") {
		if !confirm(fmt.Sprintf("delete %q on %s?", args[0], "the configured forges")) {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "aborted"); err != nil {
				return err
			}
			return nil
		}
	}

	cs, ctx, cancel, err := repoContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	fullName := args[0]
	var deleted int
	for _, c := range cs {
		if err := c.DeleteRepo(ctx, fullName); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			continue
		}
		deleted++
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: deleted %s\n", c.Name(), fullName); err != nil {
			return err
		}
	}
	if deleted == 0 {
		return fmt.Errorf("no repositories deleted: every forge failed")
	}
	return nil
}

func runRepoVisibility(cmd *cobra.Command, args []string) error {
	var private bool
	switch args[1] {
	case "public":
	case "private":
		private = true
	default:
		return fmt.Errorf("visibility must be 'public' or 'private', got %q", args[1])
	}

	cs, ctx, cancel, err := repoContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	fullName := args[0]
	var updated []operations.Repo
	for _, c := range cs {
		repo, err := c.SetVisibility(ctx, fullName, private)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			continue
		}
		updated = append(updated, repo)
		vis := "public"
		if repo.Private {
			vis = "private"
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s is now %s\n", c.Name(), repo.FullName, vis); err != nil {
			return err
		}
	}
	if len(updated) == 0 {
		return fmt.Errorf("no repositories updated: every forge failed")
	}
	return nil
}
