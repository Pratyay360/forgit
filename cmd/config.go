package cmd

import (
	"fmt"
	"io"

	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

// sampleConfig shows the supported per-forge settings in a ~/.config/forgit/config.toml file.
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

# A user-named alias of a built-in driver. The 'type' is the public label
# shown everywhere; 'driver' picks which API client handles requests. The
# URL shape must match the driver's (e.g. Gitea uses Forgejo's /api/v1).
# Useful for self-hosted servers you want labelled differently from the
# upstream driver name (Gitea, an internal Forgejo, etc.). The 'url' is
# required so the local remote can be matched back to this instance.
[[instance]]
name = "gitea-home"
type = "gitea"
driver = "forgejo"
token = "..."
url = "https://gitea.example.com"
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
	_, _ = fmt.Fprintf(out, "Config file: %s\n\n", path)
	_, _ = fmt.Fprintln(out, "Forge instances:")
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
		_, _ = fmt.Fprintf(out, "  %-10s %s\n", f.name, status)
	}
	for _, inst := range cfg.Instances {
		status := "not configured"
		if inst.Token != "" {
			status = "configured"
		}
		// Aliases show the driver so users can tell at a glance which API
		// client backs a custom label (e.g. "gitea → forgejo").
		label := inst.Type
		if inst.Driver != "" {
			label = inst.Type + " → " + inst.Driver
		}
		_, _ = fmt.Fprintf(out, "  %-10s (%s) %s\n", inst.Name, label, status)
	}
	if !cfg.Enabled() {
		_, _ = fmt.Fprintln(out, "\nNo forge credentials configured. Create the file above with per-forge")
		_, _ = fmt.Fprintln(out, "tokens, or set the FORGIT_*_TOKEN environment variables. Example:")
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprint(out, sampleConfig)
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
		return cfg, fmt.Errorf("no forge credentials configured: create %s with per-forge tokens or set the FORGIT_*_TOKEN environment variables (run 'forgit config' to see the resolved path)", path)
	}
	return cfg, nil
}
