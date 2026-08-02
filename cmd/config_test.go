package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pratyay360/forge/v1/operations"
	"github.com/spf13/cobra"
)

// clearForgeEnv empties every forge credential env var for the duration of a test.
func clearForgeEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		operations.EnvGitHubToken,
		operations.EnvGitLabToken,
		operations.EnvGitLabURL,
		operations.EnvForgejoToken,
		operations.EnvForgejoURL,
		operations.EnvSourceHutToken,
		operations.EnvSourceHutUsername,
		operations.EnvBitbucketToken,
		operations.EnvBitbucketUsername,
		operations.EnvConfigPath,
	} {
		t.Setenv(k, "")
	}
}

func newTestRoot() *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.PersistentFlags().String("config", "", "")
	return c
}

func TestLoadConfigRejectsNoCredentials(t *testing.T) {
	clearForgeEnv(t)
	t.Setenv(operations.EnvConfigPath, filepath.Join(t.TempDir(), "missing.toml"))

	if _, err := loadConfig(newTestRoot()); err == nil {
		t.Fatal("expected error when no forge credentials are configured")
	}
}

func TestLoadConfigAcceptsEnvCredentials(t *testing.T) {
	clearForgeEnv(t)
	t.Setenv(operations.EnvGitHubToken, "env-token")
	t.Setenv(operations.EnvConfigPath, filepath.Join(t.TempDir(), "missing.toml"))

	cfg, err := loadConfig(newTestRoot())
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.GitHub.Token != "env-token" {
		t.Errorf("github token = %q, want %q", cfg.GitHub.Token, "env-token")
	}
}

func TestLoadConfigAcceptsConfigFlag(t *testing.T) {
	clearForgeEnv(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "forge.toml")
	if err := os.WriteFile(path, []byte("[github]\ntoken = \"file-token\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cmd := newTestRoot()
	if err := cmd.PersistentFlags().Set("config", path); err != nil {
		t.Fatalf("setting flag: %v", err)
	}

	cfg, err := loadConfig(cmd)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.GitHub.Token != "file-token" {
		t.Errorf("github token = %q, want %q", cfg.GitHub.Token, "file-token")
	}
}

// runConfigCommand builds a cobra command wired to the given config path with
// captured output, ready for runConfig.
func runConfigCommand(t *testing.T, path string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := newTestRoot()
	if err := cmd.PersistentFlags().Set("config", path); err != nil {
		t.Fatalf("setting flag: %v", err)
	}
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	return cmd, &out, &errBuf
}

func TestRunConfigWarnsUnusedSourceHutUsername(t *testing.T) {
	clearForgeEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.toml")
	if err := os.WriteFile(path, []byte("[sourcehut]\ntoken = \"t\"\nusername = \"you\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cmd, out, errBuf := runConfigCommand(t, path)
	if err := runConfig(cmd, nil); err != nil {
		t.Fatalf("runConfig: %v", err)
	}
	if !strings.Contains(errBuf.String(), "no longer needed") {
		t.Errorf("stderr = %q, want a warning about the unused sourcehut username", errBuf.String())
	}
	if !strings.Contains(out.String(), "sourcehut  configured") {
		t.Errorf("stdout = %q, want sourcehut marked configured", out.String())
	}
}

func TestRunConfigWarnsOnEnvUsername(t *testing.T) {
	clearForgeEnv(t)
	// Username supplied via the environment must trigger the same warning.
	t.Setenv(operations.EnvSourceHutUsername, "you")
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.toml")
	if err := os.WriteFile(path, []byte("[sourcehut]\ntoken = \"t\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cmd, _, errBuf := runConfigCommand(t, path)
	if err := runConfig(cmd, nil); err != nil {
		t.Fatalf("runConfig: %v", err)
	}
	if !strings.Contains(errBuf.String(), "no longer needed") {
		t.Errorf("stderr = %q, want a warning about the unused sourcehut username", errBuf.String())
	}
}

func TestRunConfigNoWarningWithoutUsername(t *testing.T) {
	clearForgeEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.toml")
	if err := os.WriteFile(path, []byte("[sourcehut]\ntoken = \"t\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cmd, _, errBuf := runConfigCommand(t, path)
	if err := runConfig(cmd, nil); err != nil {
		t.Fatalf("runConfig: %v", err)
	}
	if strings.Contains(errBuf.String(), "no longer needed") {
		t.Errorf("stderr = %q, want no warning when username is absent", errBuf.String())
	}
}

func TestSampleConfigSourceHutHasNoUsername(t *testing.T) {
	// The SourceHut section of the sample must not advertise a username; the
	// GraphQL API resolves it from the token.
	start := strings.Index(sampleConfig, "[sourcehut]")
	end := strings.Index(sampleConfig, "[bitbucket]")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("sampleConfig missing [sourcehut] or [bitbucket] sections:\n%s", sampleConfig)
	}
	if strings.Contains(sampleConfig[start:end], "username") {
		t.Errorf("sourcehut section still mentions username:\n%s", sampleConfig[start:end])
	}
}
