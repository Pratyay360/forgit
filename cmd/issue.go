package cmd

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

// issueCmd is the parent for all issue operations.
var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Work with issues (list, view, create, comment, close)",
}

func init() {
	rootCmd.AddCommand(issueCmd)
	issueCmd.AddCommand(issueListCmd, issueViewCmd, issueCreateCmd, issueCommentCmd, issueCloseCmd)
}

var issueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List open issues across all configured forges",
	RunE:  runIssueList,
}

func init() {
	issueListCmd.Flags().String("state", "open", "filter by state: open, closed, all")
	issueListCmd.Flags().String("repo", "", "narrow the listing to a specific owner/name")
	issueListCmd.Flags().String("instance", "", "narrow the listing to a specific configured instance")
}

func runIssueList(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	cs, err := clients(cfg)
	if err != nil {
		return err
	}
	if instFlag := flagString(cmd, "instance"); instFlag != "" {
		cs = filterClients(cs, instFlag)
		if len(cs) == 0 {
			return fmt.Errorf("no configured instance named %q", instFlag)
		}
	}

	ctx, cancel := withTimeout(cmd)
	defer cancel()

	repoFlag := flagString(cmd, "repo")
	state := flagString(cmd, "state")

	var all []operations.Issue
	failed, ok := 0, 0
	for _, c := range cs {
		issues, err := c.ListIssues(ctx)
		if err != nil {
			if errors.Is(err, operations.ErrIssuesUnsupported) {
				fmt.Fprintf(cmd.ErrOrStderr(), "note: %s: %v\n", c.Name(), err)
				continue
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %v\n", c.Name(), err)
			failed++
			continue
		}
		ok++
		for _, is := range issues {
			if !matchesState(is.State, state) {
				continue
			}
			if repoFlag != "" && is.Repo != repoFlag {
				continue
			}
			all = append(all, is)
		}
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
	if err := writeTable(cmd.OutOrStdout(), []string{"FORGE", "REPOSITORY", "#", "STATE", "TITLE", "URL"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%d issues from %d forges\n", len(all), ok)
	return nil
}

var issueViewCmd = &cobra.Command{
	Use:   "view NUMBER",
	Short: "View a single issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueView,
}

func init() { registerTargetFlags(issueViewCmd) }

func runIssueView(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	reader, err := resolveCapability[issueReader](t.Client, "view issues")
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()

	is, err := reader.GetIssue(ctx, t.RepoName(), number)
	if err != nil {
		return err
	}
	renderIssueDetail(cmd.OutOrStdout(), is)
	return nil
}

// renderIssueDetail formats an issue for display.
func renderIssueDetail(out io.Writer, is operations.IssueDetail) {
	if is.State == "" {
		fmt.Fprintln(out, is.Title)
		return
	}
	fmt.Fprintf(out, "%s\n", is.Title)
	fmt.Fprintf(out, "%s #%d  %s  %s\n", is.URL, is.Number, is.State, strings.ToLower(is.Forge))
	if is.Author != "" {
		fmt.Fprintf(out, "author: %s\n", is.Author)
	}
	if len(is.Labels) > 0 {
		fmt.Fprintf(out, "labels: %s\n", strings.Join(is.Labels, ", "))
	}
	if len(is.Assignees) > 0 {
		fmt.Fprintf(out, "assigned: %s\n", strings.Join(is.Assignees, ", "))
	}
	if is.Body != "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, is.Body)
	}
}

var issueCreateCmd = &cobra.Command{
	Use:   "create TITLE",
	Short: "Open a new issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueCreate,
}

func init() {
	registerTargetFlags(issueCreateCmd)
	issueCreateCmd.Flags().String("body", "", "issue body; use - to read from stdin")
	issueCreateCmd.Flags().StringSlice("labels", nil, "label names to apply (forges that ignore labels do so silently)")
}

func runIssueCreate(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	writer, err := resolveCapability[issueWriter](t.Client, "create issues")
	if err != nil {
		return err
	}
	body := flagString(cmd, "body")
	if body == "-" {
		if err := readAllStdinInto(&body); err != nil {
			return err
		}
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()

	is, err := writer.CreateIssue(ctx, operations.IssueInput{
		Repo:   t.RepoName(),
		Title:  args[0],
		Body:   body,
		Labels: stringSliceFlag(cmd, "labels"),
	})
	if err != nil {
		return err
	}
	renderIssueDetail(cmd.OutOrStdout(), is)
	return nil
}

var issueCommentCmd = &cobra.Command{
	Use:   "comment NUMBER [TEXT...]",
	Short: "Add a comment to an issue",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runIssueComment,
}

func init() { registerTargetFlags(issueCommentCmd) }

func runIssueComment(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	writer, err := resolveCapability[issueWriter](t.Client, "comment on issues")
	if err != nil {
		return err
	}
	body := bodyFromArgs(args[1:])
	if body == "" {
		return fmt.Errorf("comment body is empty; supply it as arguments or pipe stdin")
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()
	return writer.CommentIssue(ctx, t.RepoName(), number, body)
}

var issueCloseCmd = &cobra.Command{
	Use:   "close NUMBER",
	Short: "Close an issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueClose,
}

func init() { registerTargetFlags(issueCloseCmd) }

func runIssueClose(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	writer, err := resolveCapability[issueWriter](t.Client, "close issues")
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()
	return writer.CloseIssue(ctx, t.RepoName(), number)
}

// stringSliceFlag returns a string slice flag's value, or nil when absent.
func stringSliceFlag(cmd *cobra.Command, name string) []string {
	if cmd.Flags().Lookup(name) == nil {
		return nil
	}
	v, _ := cmd.Flags().GetStringSlice(name)
	return v
}
