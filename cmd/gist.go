package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

// gistCmd is the parent for gist operations.
var gistCmd = &cobra.Command{
	Use:   "gist",
	Short: "Work with GitHub Gists",
}

func init() {
	rootCmd.AddCommand(gistCmd)
	gistCmd.AddCommand(gistCreateCmd)
}

var gistCreateCmd = &cobra.Command{
	Use:   "create [TITLE]",
	Short: "Create a new GitHub Gist",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runGistCreate,
}

func init() {
	gistCreateCmd.Flags().Bool("public", true, "create a public gist (default: true)")
	gistCreateCmd.Flags().Bool("private", false, "create a private gist")
	gistCreateCmd.Flags().String("description", "", "gist description")
	gistCreateCmd.Flags().StringSlice("file", nil, "file name and content as 'name:content' (repeatable)")
}

func runGistCreate(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	if cfg.GitHub.Token == "" {
		return fmt.Errorf("github token not configured; set FORGIT_GITHUB_TOKEN or add [github] token to config")
	}

	title := ""
	if len(args) > 0 {
		title = args[0]
	}

	files := make(map[string]string)
	fileFlags := stringSliceFlag(cmd, "file")
	for _, f := range fileFlags {
		parts := strings.SplitN(f, ":", 2)
		if len(parts) == 2 {
			files[parts[0]] = parts[1]
		} else {
			files[f] = ""
		}
	}

	// If --file was not used, read from stdin if data is piped in.
	if len(fileFlags) == 0 {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			content, _ := os.ReadFile("/dev/stdin")
			name := title
			if name == "" {
				name = "gistfile.txt"
			}
			files[name] = string(content)
		}
	}

	if len(files) == 0 {
		return fmt.Errorf("no files specified; use --file name:content or pipe stdin")
	}

	client := operations.New(cfg.GitHub.Token)
	gist, err := client.Create(cmd.Context(), operations.GistInput{
		Description: title,
		Public:      !boolFlag(cmd, "private"),
		Files:       files,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", gist.URL, gist.Description); err != nil {
		return err
	}
	return nil
}
