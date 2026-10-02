package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pratyay360/forgit/internal/gitlocal"
	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

// prCmd is the parent for all pull request operations. The subcommands cover
// the lifecycle: list and view come first, then the writes (create, comment,
// close, ready, merge), then the local side (checkout, diff, web).
var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Work with pull requests (list, view, create, checkout, comment, close, merge, diff)",
	Long: `Work with pull requests across every configured forge.

For forges without a global pull request API (GitHub, Forgejo, Bitbucket)
the ten most recently listed repositories are scanned by default. The list
of repositories is refreshed on each invocation. Use --limit, --repo or
--instance to narrow the scope.`,
}

func init() {
	rootCmd.AddCommand(prCmd)
	prCmd.AddCommand(prListCmd, prViewCmd, prCreateCmd, prCheckoutCmd, prDiffCmd,
		prCommentCmd, prCloseCmd, prReadyCmd, prDraftCmd, prMergeCmd, prWebCmd)
}

// registerTargetFlags is the standard helper used by every targeted PR
// subcommand; it lives in context.go and is re-exported here as a name the
// init functions can scan for when adding new commands.

var prListCmd = &cobra.Command{
	Use:   "list",
	Short: "List open pull requests across all configured forges",
	RunE:  runPRList,
}

func init() {
	prListCmd.Flags().Int("limit", 10, "repositories to scan per forge that lacks a global pull request API")
	prListCmd.Flags().String("state", "open", "filter by state: open, closed, merged, all")
}

// runPRList keeps the cross-forge aggregation pattern but lets the caller
// narrow it with --repo, --instance and --state.
func runPRList(cmd *cobra.Command, args []string) error {
	repoFlag := flagString(cmd, "repo")
	instFlag := flagString(cmd, "instance")

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	cs, err := clients(cfg)
	if err != nil {
		return err
	}
	if instFlag != "" {
		cs = filterClients(cs, instFlag)
		if len(cs) == 0 {
			return fmt.Errorf("no configured instance named %q", instFlag)
		}
	}

	ctx, cancel := withTimeout(cmd)
	defer cancel()

	state := flagString(cmd, "state")

	var all []operations.PR
	failed, ok := 0, 0
	for _, c := range cs {
		prs, err := c.ListPRs(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			failed++
			continue
		}
		ok++
		for _, pr := range prs {
			if !matchesState(pr.State, state) {
				continue
			}
			if repoFlag != "" && pr.Repo != repoFlag {
				continue
			}
			all = append(all, pr)
		}
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
	if err := writeTable(cmd.OutOrStdout(), []string{"FORGE", "REPOSITORY", "#", "STATE", "TITLE", "URL"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%d pull requests from %d forges\n", len(all), ok)
	return nil
}

// matchesState reports whether a PR's state matches the filter.
func matchesState(state, want string) bool {
	switch want {
	case "", "all":
		return true
	case "merged":
		return state == "merged" || state == "closed"
	default:
		return state == want
	}
}

var prViewCmd = &cobra.Command{
	Use:   "view NUMBER",
	Short: "View a single pull request",
	Args:  cobra.ExactArgs(1),
	RunE:  runPRView,
}

func init() {
	registerTargetFlags(prViewCmd)
}

func runPRView(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	reader, err := resolveCapability[prReader](t.Client, "view pull requests")
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()

	pr, err := reader.GetPR(ctx, t.RepoName(), number)
	if err != nil {
		return err
	}
	renderPRDetail(cmd.OutOrStdout(), pr)
	return nil
}

// renderPRDetail formats a pull request for display.
func renderPRDetail(out io.Writer, pr operations.PRDetail) {
	if pr.State == "" {
		fmt.Fprintln(out, pr.Title)
		return
	}
	draft := ""
	if pr.Draft {
		draft = " (draft)"
	}
	fmt.Fprintf(out, "%s%s\n", pr.Title, draft)
	fmt.Fprintf(out, "%s #%d  %s  %s\n", pr.URL, pr.Number, pr.State, strings.ToLower(pr.Forge))
	if pr.Author != "" {
		fmt.Fprintf(out, "author: %s\n", pr.Author)
	}
	if pr.BaseBranch != "" {
		fmt.Fprintf(out, "base:  %s\n", pr.BaseBranch)
	}
	if pr.HeadBranch != "" {
		owner := pr.HeadOwner
		if owner == "" {
			owner = pr.Author
		}
		fmt.Fprintf(out, "head:  %s/%s\n", owner, pr.HeadBranch)
	}
	if pr.Body != "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, pr.Body)
	}
}

var prCreateCmd = &cobra.Command{
	Use:   "create TITLE",
	Short: "Open a pull request on the current branch",
	Args:  cobra.ExactArgs(1),
	RunE:  runPRCreate,
}

func init() {
	registerTargetFlags(prCreateCmd)
	prCreateCmd.Flags().String("base", "", "base branch; defaults to the repository default")
	prCreateCmd.Flags().String("head", "", "head branch; defaults to the current branch")
	prCreateCmd.Flags().String("body", "", "pull request body; use $EDITOR for an interactive edit (or pipe stdin when set to -)")
	prCreateCmd.Flags().StringSlice("labels", nil, "label names to apply (forges that ignore labels do so silently)")
	prCreateCmd.Flags().Bool("draft", false, "create as a draft pull request")
}

func runPRCreate(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	writer, err := resolveCapability[prWriter](t.Client, "create pull requests")
	if err != nil {
		return err
	}

	body := flagString(cmd, "body")
	if body == "-" {
		if err := readAllStdinInto(&body); err != nil {
			return err
		}
	}

	head := flagString(cmd, "head")
	if head == "" && t.Local != nil {
		head = t.Local.HeadBranch()
	}

	ctx, cancel := withTimeout(cmd)
	defer cancel()

	pr, err := writer.CreatePR(ctx, operations.PRInput{
		Repo:  t.RepoName(),
		Title: args[0],
		Body:  body,
		Base:  flagString(cmd, "base"),
		Head:  head,
		Draft: boolFlag(cmd, "draft"),
	})
	if err != nil {
		return err
	}
	renderPRDetail(cmd.OutOrStdout(), pr)
	return nil
}

var prCheckoutCmd = &cobra.Command{
	Use:   "checkout NUMBER",
	Short: "Fetch and check out a remote pull request head",
	Long: `Fetch and check out a remote pull request head into a local branch.

This works from inside a clone by default, and can also be run against a
remote fork: if the pull request comes from another repository, forgit
adds a temporary git remote pointing at the fork before fetching.`,
	Args: cobra.ExactArgs(1),
	RunE: runPRCheckout,
}

func init() {
	registerTargetFlags(prCheckoutCmd)
	prCheckoutCmd.Flags().Bool("detach", false, "check out the head commit without creating a branch")
	prCheckoutCmd.Flags().Bool("force", false, "reset an existing local branch to the pull request head")
	prCheckoutCmd.Flags().String("remote", "", "local git remote to fetch from (default: the local repo's remote, or 'origin')")
	prCheckoutCmd.Flags().String("branch", "", "local branch name to create (default: forge-specific naming)")
}

func runPRCheckout(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	if err := t.requireLocal(); err != nil {
		return err
	}

	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	fetcher, err := resolveCapability[prFetcher](t.Client, "fetch pull requests")
	if err != nil {
		return err
	}

	ctx, cancel := withTimeout(cmd)
	defer cancel()

	spec, err := fetcher.FetchSpec(ctx, t.RepoName(), number)
	if err != nil {
		return err
	}

	if flagString(cmd, "remote") != "" {
		spec.Remote = flagString(cmd, "remote")
	}
	if flagString(cmd, "branch") != "" {
		spec.LocalBranch = flagString(cmd, "branch")
	}

	// A fork pull request comes with a clone URL; add it as a transient remote
	// so the fetch can find the head branch.
	if spec.ForkURL != "" {
		forkName := "forgit-fork-" + t.Bound.Forge
		if err := t.Local.AddRemote(forkName, spec.ForkURL); err != nil {
			return err
		}
		spec.Remote = forkName
		defer func() { _ = t.Local.RemoveRemote(forkName) }()
	}

	if err := t.Local.CheckoutPR(ctx, t.Auth, gitlocal.CheckoutOptions{
		Spec:   spec,
		Detach: boolFlag(cmd, "detach"),
		Force:  boolFlag(cmd, "force"),
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "checked out %s #%d as %s\n", t.Bound.Forge, number, describeLocalBranch(spec, boolFlag(cmd, "detach")))
	return nil
}

// describeLocalBranch names the branch the checkout landed on.
func describeLocalBranch(spec operations.FetchSpec, detach bool) string {
	if detach {
		short := spec.HeadSHA
		if len(short) > 7 {
			short = short[:7]
		}
		return short
	}
	if spec.LocalBranch == "" {
		return "HEAD"
	}
	return spec.LocalBranch
}

var prDiffCmd = &cobra.Command{
	Use:   "diff NUMBER",
	Short: "Print the unified diff of a pull request",
	Args:  cobra.ExactArgs(1),
	RunE:  runPRDiff,
}

func init() {
	registerTargetFlags(prDiffCmd)
}

func runPRDiff(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	differ, err := resolveCapability[prDiffer](t.Client, "view pull request diffs")
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()
	diff, err := differ.PRDiff(ctx, t.RepoName(), number)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write([]byte(diff))
	return err
}

var prCommentCmd = &cobra.Command{
	Use:   "comment NUMBER [TEXT...]",
	Short: "Add a comment to a pull request",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runPRComment,
}

func init() {
	registerTargetFlags(prCommentCmd)
}

func runPRComment(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	writer, err := resolveCapability[prWriter](t.Client, "comment on pull requests")
	if err != nil {
		return err
	}
	body := bodyFromArgs(args[1:])
	if body == "" {
		return fmt.Errorf("comment body is empty; supply it as arguments or pipe stdin")
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()
	return writer.CommentPR(ctx, t.RepoName(), number, body)
}

var prCloseCmd = &cobra.Command{
	Use:   "close NUMBER",
	Short: "Close a pull request without merging",
	Args:  cobra.ExactArgs(1),
	RunE:  runPRClose,
}

func init() {
	registerTargetFlags(prCloseCmd)
}

func runPRClose(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	writer, err := resolveCapability[prWriter](t.Client, "close pull requests")
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()
	return writer.ClosePR(ctx, t.RepoName(), number)
}

var prReadyCmd = &cobra.Command{
	Use:   "ready NUMBER",
	Short: "Mark a draft pull request as ready for review",
	Args:  cobra.ExactArgs(1),
	RunE:  runPRReady,
}

var prDraftCmd = &cobra.Command{
	Use:   "draft NUMBER",
	Short: "Convert a pull request back to draft",
	Args:  cobra.ExactArgs(1),
	RunE:  runPRDraft,
}

func init() {
	registerTargetFlags(prReadyCmd)
	registerTargetFlags(prDraftCmd)
}

func runPRReady(cmd *cobra.Command, args []string) error {
	return runPRReadiness(cmd, args, true)
}

func runPRDraft(cmd *cobra.Command, args []string) error {
	return runPRReadiness(cmd, args, false)
}

func runPRReadiness(cmd *cobra.Command, args []string, ready bool) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	checker, err := resolveCapability[prChecker](t.Client, "toggle draft pull requests")
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()
	return checker.SetPRReady(ctx, t.RepoName(), number, ready)
}

var prMergeCmd = &cobra.Command{
	Use:   "merge NUMBER",
	Short: "Merge a pull request",
	Args:  cobra.ExactArgs(1),
	RunE:  runPRMerge,
}

func init() {
	registerTargetFlags(prMergeCmd)
	prMergeCmd.Flags().String("method", "", "merge method: merge, squash, rebase (only where the forge accepts per-merge overrides)")
	prMergeCmd.Flags().Bool("delete-branch", false, "delete the head branch after the merge")
	prMergeCmd.Flags().String("subject", "", "commit title to use for the merge commit")
	prMergeCmd.Flags().String("body", "", "commit message body for the merge commit")
}

func runPRMerge(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	number, err := parseNumber(args[0])
	if err != nil {
		return err
	}
	writer, err := resolveCapability[prWriter](t.Client, "merge pull requests")
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()
	return writer.MergePR(ctx, t.RepoName(), number, operations.MergeOptions{
		MergeMethod:     flagString(cmd, "method"),
		DeleteBranch:    boolFlag(cmd, "delete-branch"),
		Title:           flagString(cmd, "subject"),
		OptionalMessage: flagString(cmd, "body"),
	})
}

var prWebCmd = &cobra.Command{
	Use:   "web [NUMBER]",
	Short: "Open the pull request in a browser",
	Args:  cobra.RangeArgs(0, 1),
	RunE:  runPRWeb,
}

func init() {
	registerTargetFlags(prWebCmd)
}

func runPRWeb(cmd *cobra.Command, args []string) error {
	t, err := resolveTarget(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(cmd)
	defer cancel()

	url := ""
	if len(args) == 1 {
		number, err := parseNumber(args[0])
		if err != nil {
			return err
		}
		reader, err := resolveCapability[prReader](t.Client, "view pull requests")
		if err != nil {
			return err
		}
		pr, err := reader.GetPR(ctx, t.RepoName(), number)
		if err != nil {
			return err
		}
		url = pr.URL
	} else {
		prs, err := t.Client.ListPRs(ctx)
		if err != nil {
			return err
		}
		if len(prs) == 0 {
			return fmt.Errorf("no pull requests found for %s", t.RepoName())
		}
		url = prs[0].URL
	}
	return openURLFn(url)
}

// readAllStdinInto reads stdin into the named string pointer. It is only used
// when the user passes - as the body of `forgit pr create`.
func readAllStdinInto(s *string) error {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	*s = string(b)
	return nil
}

// ensure unused imports do not break a build before the next batch of files.
var _ time.Duration = time.Second
