package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/pratyay360/forgit/internal/jjclient"
	"github.com/spf13/cobra"
)

// gitCmd is the parent for local git operations. The subcommands mirror the
// short-form git commands enough to make the rest of forgit usable, without
// re-implementing git itself: status, log, diff, branch, remote.
var gitCmd = &cobra.Command{
	Use:   "git",
	Short: "Local git operations (status, log, diff, branch, remote)",
	Long: `Inspect a local repository without leaving forgit.

Pull and push are deliberately omitted: the local checkout of a remote pull
request is handled by 'forgit pr checkout', and pushing is best left to git
itself so credentials, hooks and LFS keep working out of the box.`,
}

func init() {
	rootCmd.AddCommand(gitCmd)
	gitCmd.AddCommand(gitStatusCmd, gitLogCmd, gitDiffCmd, gitBranchCmd, gitRemoteCmd)
}

var gitStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the working tree status",
	RunE:  runGitStatus,
}

func init() {
	gitStatusCmd.Flags().String("dir", "", "working directory (default: the current directory)")
}

func runGitStatus(cmd *cobra.Command, args []string) error {
	repo, err := openLocalRepo(cmd)
	if err != nil {
		return err
	}
	changes, err := repo.Status()
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "clean")
		return nil
	}
	for _, c := range changes {
		fmt.Fprintln(cmd.OutOrStdout(), c.String())
	}
	return nil
}

// openLocalRepo opens the repository rooted at dir (default .).
func openLocalRepo(cmd *cobra.Command) (*jjclient.Repo, error) {
	return jjclient.Open(workDir(cmd))
}

var gitLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent commits",
	RunE:  runGitLog,
}

func init() {
	gitLogCmd.Flags().Int("limit", 10, "max number of commits")
	gitLogCmd.Flags().String("path", "", "limit to commits touching this path")
}

func runGitLog(cmd *cobra.Command, args []string) error {
	repo, err := openLocalRepo(cmd)
	if err != nil {
		return err
	}
	entries, err := repo.Log(flagInt(cmd, "limit"), flagString(cmd, "path"))
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %s\n", e.Short, e.When.Format("2006-01-02"), e.Author, e.Subject)
	}
	return nil
}

var gitDiffCmd = &cobra.Command{
	Use:   "diff [FROM] [TO]",
	Short: "Show the diff between two revisions (default: HEAD vs working tree)",
	Args:  cobra.RangeArgs(0, 2),
	RunE:  runGitDiff,
}

func init() {
	gitDiffCmd.Flags().String("dir", "", "working directory (default: the current directory)")
}

func runGitDiff(cmd *cobra.Command, args []string) error {
	repo, err := openLocalRepo(cmd)
	if err != nil {
		return err
	}
	from := ""
	to := ""
	if len(args) >= 1 {
		from = args[0]
	}
	if len(args) == 2 {
		to = args[1]
	}
	out, err := repo.Diff(from, to)
	if err != nil {
		return err
	}
	_, err = io.WriteString(cmd.OutOrStdout(), out)
	return err
}

var gitBranchCmd = &cobra.Command{
	Use:   "branch",
	Short: "List branches",
	RunE:  runGitBranch,
}

func runGitBranch(cmd *cobra.Command, args []string) error {
	repo, err := openLocalRepo(cmd)
	if err != nil {
		return err
	}
	branches, err := repo.Branches()
	if err != nil {
		return err
	}
	for _, b := range branches {
		marker := " "
		if b.IsHead {
			marker = "*"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", marker, b.Short)
	}
	return nil
}

var gitRemoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "List configured remotes",
	RunE:  runGitRemote,
}

func init() {
	gitRemoteCmd.Flags().Bool("add", false, "add a remote: forgit git remote --add NAME URL")
	gitRemoteCmd.Flags().Bool("remove", false, "remove a remote: forgit git remote --remove NAME")
	gitRemoteCmd.Flags().String("url", "", "URL used with --add")
	gitRemoteCmd.Flags().String("name", "", "name used with --add or --remove")
}

func runGitRemote(cmd *cobra.Command, args []string) error {
	repo, err := openLocalRepo(cmd)
	if err != nil {
		return err
	}
	switch {
	case boolFlag(cmd, "add"):
		name, url := flagString(cmd, "name"), flagString(cmd, "url")
		if name == "" || url == "" {
			return fmt.Errorf("--add needs --name and --url")
		}
		if err := repo.AddRemote(name, url); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "added remote %s\n", name)
		return nil
	case boolFlag(cmd, "remove"):
		name := flagString(cmd, "name")
		if name == "" {
			return fmt.Errorf("--remove needs --name")
		}
		if err := repo.RemoveRemote(name); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "removed remote %s\n", name)
		return nil
	}
	remotes, err := repo.Remotes()
	if err != nil {
		return err
	}
	for _, r := range remotes {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", r.Name, r.URL())
	}
	return nil
}

// flagInt returns an int flag's value, or 0 when absent.
func flagInt(cmd *cobra.Command, name string) int {
	if cmd.Flags().Lookup(name) == nil {
		return 0
	}
	v, _ := cmd.Flags().GetInt(name)
	return v
}

var _ = os.Stdout