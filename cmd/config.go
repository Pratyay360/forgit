package cmd

import (
	"fmt"
	"io"

	"github.com/pratyay360/forge/v1/operations"
	"github.com/spf13/cobra"
)

// sampleConfig shows the supported per-forge settings in a ~/.forge.toml file.
const sampleConfig = `[github]
token = "..."

[gitlab]
token = "..."
url = "https://gitlab.example.com"  # optional, for self-hosted instances

[forgejo]
token = "..."
url = "https://codeberg.org"  # optional, defaults to codeberg.org

[sourcehut]
token = "..."

[bitbucket]
token = "..."
username = "you"

# Additional instances of any forge type (e.g. a second GitLab server):
[[instance]]
name = "gitlab-work"
type = "gitlab"
token = "..."
url = "https://gitlab.example.com"
`

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show the config file location and which forges are configured",
	RunE:  runConfig,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) error {
	path, err := operations.ResolveConfigPath(configFlag(cmd))
	if err != nil {
		return err
	}
	cfg, err := operations.LoadConfig(path)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Config file: %s\n\n", path)
	fmt.Fprintln(out, "Forge instances:")
	for _, f := range []struct {
		name  string
		ready bool
	}{
		{"github", cfg.GitHub.Token != ""},
		{"gitlab", cfg.GitLab.Token != ""},
		{"forgejo", cfg.Forgejo.Token != ""},
		{"sourcehut", cfg.SourceHut.Token != ""},
		{"bitbucket", cfg.Bitbucket.Token != ""},
	} {
		status := "not configured"
		if f.ready {
			status = "configured"
		}
		fmt.Fprintf(out, "  %-10s %s\n", f.name, status)
	}
	for _, inst := range cfg.Instances {
		status := "not configured"
		if inst.Token != "" {
			status = "configured"
		}
		fmt.Fprintf(out, "  %-10s (%s) %s\n", inst.Name, inst.Type, status)
	}
	if !cfg.Enabled() {
		fmt.Fprintln(out, "\nNo forge credentials configured. Create the file above with per-forge")
		fmt.Fprintln(out, "tokens, or set the FORGE_*_TOKEN environment variables. Example:")
		fmt.Fprintln(out)
		fmt.Fprint(out, sampleConfig)
	}
	warnUnusedSourceHutUsername(cmd.ErrOrStderr(), cfg)
	return nil
}

// warnUnusedSourceHutUsername notes when a [sourcehut] username is configured
// but no longer needed: the SourceHut GraphQL API resolves the authenticated
// user from the token itself, so the username is dead configuration.
func warnUnusedSourceHutUsername(w io.Writer, cfg operations.Config) {
	if cfg.SourceHut.Username == "" {
		return
	}
	fmt.Fprintf(w, "note: sourcehut: username is no longer needed — the GraphQL API resolves your account from the token. Remove it from [sourcehut] or unset %s.\n", operations.EnvSourceHutUsername)
}

// configFlag returns the value of the global --config flag. During real cobra
// execution the persistent flag is merged into cmd.Flags(); the persistent flag
// set is checked as a fallback so callers (and tests) work without execution.
func configFlag(cmd *cobra.Command) string {
	if v, err := cmd.Flags().GetString("config"); err == nil && v != "" {
		return v
	}
	if v, err := cmd.PersistentFlags().GetString("config"); err == nil {
		return v
	}
	return ""
}

// loadConfig resolves and loads the config, failing with setup help when no
// forge has credentials configured.
func loadConfig(cmd *cobra.Command) (operations.Config, error) {
	path, err := operations.ResolveConfigPath(configFlag(cmd))
	if err != nil {
		return operations.Config{}, err
	}
	cfg, err := operations.LoadConfig(path)
	if err != nil {
		return cfg, err
	}
	if !cfg.Enabled() {
		return cfg, fmt.Errorf("no forge credentials configured: create %s with per-forge tokens or set the FORGE_*_TOKEN environment variables (run 'forge config' to see the resolved path)", path)
	}
	return cfg, nil
}
