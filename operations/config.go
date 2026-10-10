package operations

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds credentials and settings for every supported forge.
type Config struct {
	GitHub    GitHubConfig    `toml:"github"`
	GitLab    GitLabConfig    `toml:"gitlab"`
	Forgejo   ForgejoConfig   `toml:"forgejo"`
	SourceHut SourceHutConfig `toml:"sourcehut"`
	Bitbucket BitbucketConfig `toml:"bitbucket"`
	Instances []Instance      `toml:"instance"`
}

type Instance struct {
	Name      string `toml:"name"`
	Type      string `toml:"type"`
	Driver    string `toml:"driver"`
	Token     string `toml:"token"`
	URL       string `toml:"url"`
	Username  string `toml:"username"`
	Workspace string `toml:"workspace"`
}

// knownDrivers is the set of values Driver may take. Keeping it private lets
// the public API stay small while still validating user input.
var knownDrivers = map[string]bool{
	"github":    true,
	"gitlab":    true,
	"forgejo":   true,
	"sourcehut": true,
	"bitbucket": true,
}

type GitHubConfig struct {
	Token string `toml:"token"`
}

type GitLabConfig struct {
	Token string `toml:"token"`
	URL   string `toml:"url"`
}

type ForgejoConfig struct {
	Token string `toml:"token"`
	URL   string `toml:"url"`
}

type SourceHutConfig struct {
	Token    string `toml:"token"`
	Username string `toml:"username"`
}

type BitbucketConfig struct {
	Token     string `toml:"token"`
	Username  string `toml:"username"`
	Workspace string `toml:"workspace"`
}

const (
	EnvConfigPath         = "FORGIT_CONFIG"
	EnvGitHubToken        = "FORGIT_GITHUB_TOKEN"
	EnvGitLabToken        = "FORGIT_GITLAB_TOKEN"
	EnvGitLabURL          = "FORGIT_GITLAB_URL"
	EnvForgejoToken       = "FORGIT_FORGEJO_TOKEN"
	EnvForgejoURL         = "FORGIT_FORGEJO_URL"
	EnvSourceHutToken     = "FORGIT_SOURCEHUT_TOKEN"
	EnvSourceHutUsername  = "FORGIT_SOURCEHUT_USERNAME"
	EnvBitbucketToken     = "FORGIT_BITBUCKET_TOKEN"
	EnvBitbucketUsername  = "FORGIT_BITBUCKET_USERNAME"
	EnvBitbucketWorkspace = "FORGIT_BITBUCKET_WORKSPACE"
)

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".config/forgit/config.toml"), nil
}

// ResolveConfigPath returns the config path from a --config flag, the
// FORGIT_CONFIG environment variable, or the default location, in that order.
func ResolveConfigPath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if p := os.Getenv(EnvConfigPath); p != "" {
		return p, nil
	}
	return DefaultConfigPath()
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if !os.IsNotExist(err) {
			return cfg, fmt.Errorf("reading config %s: %w", path, err)
		}
	}
	applyEnv(&cfg)
	if err := resolveTokens(&cfg); err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate checks that every configured instance points at a known driver.
// Aliases (entries with Driver) are validated against knownDrivers; entries
// without Driver fall through to the existing per-forge-config path, which
// already filters out sections with no token via All(). The function is
// intentionally narrow because the variable-name vocabulary grows with each
// forge that gets added.
func (c Config) Validate() error {
	for _, i := range c.Instances {
		if i.Driver != "" && !knownDrivers[i.Driver] {
			return fmt.Errorf("instance %q: unknown driver %q (must be one of github, gitlab, forgejo, sourcehut, bitbucket)", i.Name, i.Driver)
		}
	}
	return nil
}

func resolveTokens(cfg *Config) error {
	var err error
	cfg.GitHub.Token, err = resolveToken(cfg.GitHub.Token)
	if err != nil {
		return err
	}
	cfg.GitLab.Token, err = resolveToken(cfg.GitLab.Token)
	if err != nil {
		return err
	}
	cfg.Forgejo.Token, err = resolveToken(cfg.Forgejo.Token)
	if err != nil {
		return err
	}
	cfg.SourceHut.Token, err = resolveToken(cfg.SourceHut.Token)
	if err != nil {
		return err
	}
	cfg.Bitbucket.Token, err = resolveToken(cfg.Bitbucket.Token)
	if err != nil {
		return err
	}
	for i := range cfg.Instances {
		cfg.Instances[i].Token, err = resolveToken(cfg.Instances[i].Token)
		if err != nil {
			return err
		}
	}
	return nil
}

func resolveToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	isCommand := strings.Contains(token, " ") ||
		strings.Contains(token, "$") ||
		strings.HasPrefix(token, "echo") ||
		strings.ContainsAny(token, "|&;<>()`\\")

	if !isCommand {
		return token, nil
	}

	cmd := exec.Command("/bin/sh", "-c", token)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("evaluating token command %q: %w (stderr: %q)", token, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv(EnvGitHubToken); v != "" {
		cfg.GitHub.Token = v
	}
	if v := os.Getenv(EnvGitLabToken); v != "" {
		cfg.GitLab.Token = v
	}
	if v := os.Getenv(EnvGitLabURL); v != "" {
		cfg.GitLab.URL = v
	}
	if v := os.Getenv(EnvForgejoToken); v != "" {
		cfg.Forgejo.Token = v
	}
	if v := os.Getenv(EnvForgejoURL); v != "" {
		cfg.Forgejo.URL = v
	}
	if v := os.Getenv(EnvSourceHutToken); v != "" {
		cfg.SourceHut.Token = v
	}
	if v := os.Getenv(EnvSourceHutUsername); v != "" {
		cfg.SourceHut.Username = v
	}
	if v := os.Getenv(EnvBitbucketToken); v != "" {
		cfg.Bitbucket.Token = v
	}
	if v := os.Getenv(EnvBitbucketUsername); v != "" {
		cfg.Bitbucket.Username = v
	}
	if v := os.Getenv(EnvBitbucketWorkspace); v != "" {
		cfg.Bitbucket.Workspace = v
	}
}

// Enabled reports whether any forge has credentials configured.
func (c Config) Enabled() bool {
	return c.GitHub.Token != "" ||
		c.GitLab.Token != "" ||
		c.Forgejo.Token != "" ||
		c.SourceHut.Token != "" ||
		c.Bitbucket.Token != "" ||
		anyInstance(c.Instances, func(i Instance) bool { return i.Token != "" })
}

// All returns every configured instance: the per-forge sections (named after
// the forge type) followed by the [[instance]] entries. Sections with no token
// are skipped; [[instance]] entries without a name fall back to their type and
// entries without a token are skipped.
func (c Config) All() []Instance {
	out := make([]Instance, 0, 5+len(c.Instances))
	add := func(i Instance) {
		if i.Token == "" {
			return
		}
		if i.Name == "" {
			i.Name = i.Type
		}
		out = append(out, i)
	}
	add(Instance{Type: "github", Name: "github", Token: c.GitHub.Token})
	add(Instance{Type: "gitlab", Name: "gitlab", Token: c.GitLab.Token, URL: c.GitLab.URL})
	add(Instance{Type: "forgejo", Name: "forgejo", Token: c.Forgejo.Token, URL: c.Forgejo.URL})
	add(Instance{Type: "sourcehut", Name: "sourcehut", Token: c.SourceHut.Token, Username: c.SourceHut.Username})
	add(Instance{Type: "bitbucket", Name: "bitbucket", Token: c.Bitbucket.Token, Username: c.Bitbucket.Username, Workspace: c.Bitbucket.Workspace})
	for _, i := range c.Instances {
		add(i)
	}
	return out
}

func anyInstance(instances []Instance, f func(Instance) bool) bool {
	for _, i := range instances {
		if f(i) {
			return true
		}
	}
	return false
}
