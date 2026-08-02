package operations

import (
	"os"
	"path/filepath"
	"testing"
)

const testConfigContent = `
[github]
token = "file-gh"

[gitlab]
token = "file-gl"
url = "https://gitlab.example.com"

[forgejo]
token = "file-fj"
url = "https://codeberg.org"

[sourcehut]
token = "file-sh"
username = "alice"

[bitbucket]
token = "file-bb"
username = "bob"
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "forge.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestLoadConfigReadsFile(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, testConfigContent))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	tests := []struct {
		got  string
		want string
	}{
		{cfg.GitHub.Token, "file-gh"},
		{cfg.GitLab.Token, "file-gl"},
		{cfg.GitLab.URL, "https://gitlab.example.com"},
		{cfg.Forgejo.Token, "file-fj"},
		{cfg.Forgejo.URL, "https://codeberg.org"},
		{cfg.SourceHut.Token, "file-sh"},
		{cfg.SourceHut.Username, "alice"},
		{cfg.Bitbucket.Token, "file-bb"},
		{cfg.Bitbucket.Username, "bob"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

func TestLoadConfigEnvOverridesFile(t *testing.T) {
	path := writeConfig(t, testConfigContent)

	t.Setenv(EnvGitHubToken, "env-gh")
	t.Setenv(EnvBitbucketUsername, "env-bb-user")
	t.Setenv(EnvForgejoURL, "https://forgejo.example.com")
	t.Setenv(EnvGitLabURL, "https://gitlab.override.com")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	tests := []struct {
		got  string
		want string
	}{{cfg.GitHub.Token, "env-gh"}, // overridden by env
		{cfg.GitLab.Token, "file-gl"},                    // untouched
		{cfg.GitLab.URL, "https://gitlab.override.com"},  // overridden by env
		{cfg.Forgejo.Token, "file-fj"},                   // untouched
		{cfg.Forgejo.URL, "https://forgejo.example.com"}, // overridden by env
		{cfg.SourceHut.Username, "alice"},                // untouched
		{cfg.Bitbucket.Username, "env-bb-user"},          // overridden by env
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

func TestLoadConfigMissingFileUsesEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")

	t.Setenv(EnvGitLabToken, "env-gl")
	t.Setenv(EnvGitHubToken, "")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.GitLab.Token != "env-gl" {
		t.Errorf("gitlab token = %q, want %q", cfg.GitLab.Token, "env-gl")
	}
	if !cfg.Enabled() {
		t.Error("config with env token should be enabled")
	}
}

func TestLoadConfigInvalidTOML(t *testing.T) {
	if _, err := LoadConfig(writeConfig(t, "this is not [valid toml")); err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestConfigEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"empty", Config{}, false},
		{"github only", Config{GitHub: GitHubConfig{Token: "x"}}, true},
		{"gitlab only", Config{GitLab: GitLabConfig{Token: "x"}}, true},
		{"forgejo only", Config{Forgejo: ForgejoConfig{Token: "x"}}, true},
		{"sourcehut only", Config{SourceHut: SourceHutConfig{Token: "x"}}, true},
		{"bitbucket only", Config{Bitbucket: BitbucketConfig{Token: "x"}}, true},
		{"instance only", Config{Instances: []Instance{{Type: "gitlab", Token: "x"}}}, true},
		{"instance without token", Config{Instances: []Instance{{Type: "gitlab"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigAll(t *testing.T) {
	cfg := Config{
		GitHub: GitHubConfig{Token: "gh"},
		GitLab: GitLabConfig{Token: "gl", URL: "https://gl.example.com"},
		Instances: []Instance{
			{Name: "work", Type: "gitlab", Token: "gl2", URL: "https://gl2.example.com"},
			{Name: "", Type: "forgejo", Token: "fj"},   // name falls back to type
			{Name: "empty", Type: "github", Token: ""}, // no token -> skipped
		},
	}
	got := cfg.All()
	want := []Instance{
		{Type: "github", Name: "github", Token: "gh"},
		{Type: "gitlab", Name: "gitlab", Token: "gl", URL: "https://gl.example.com"},
		{Type: "gitlab", Name: "work", Token: "gl2", URL: "https://gl2.example.com"},
		{Type: "forgejo", Name: "forgejo", Token: "fj"},
	}
	if len(got) != len(want) {
		t.Fatalf("All() returned %d instances, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("instances[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadConfigReadsInstances(t *testing.T) {
	content := `
[github]
token = "gh"

[[instance]]
name = "work"
type = "gitlab"
token = "gl"
url = "https://gl.example.com"
`
	cfg, err := LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.GitHub.Token != "gh" {
		t.Errorf("github token = %q, want gh", cfg.GitHub.Token)
	}
	if len(cfg.Instances) != 1 {
		t.Fatalf("got %d instances, want 1", len(cfg.Instances))
	}
	inst := cfg.Instances[0]
	if inst.Name != "work" || inst.Type != "gitlab" || inst.Token != "gl" || inst.URL != "https://gl.example.com" {
		t.Errorf("instance = %+v, want work gitlab gl", inst)
	}
}

func TestResolveConfigPath(t *testing.T) {
	defaultPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}

	tests := []struct {
		name      string
		flagValue string
		envValue  string
		want      string
	}{
		{"flag wins over env", "/flag/path", "/env/path", "/flag/path"},
		{"env wins over default", "", "/env/path", "/env/path"},
		{"default path", "", "", defaultPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvConfigPath, tt.envValue)
			got, err := ResolveConfigPath(tt.flagValue)
			if err != nil {
				t.Fatalf("ResolveConfigPath: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveConfigPath(%q) = %q, want %q", tt.flagValue, got, tt.want)
			}
		})
	}
}
