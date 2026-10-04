package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pratyay360/forgit/internal/jjclient"
	"github.com/spf13/cobra"
)

// target describes what a command is acting on: one forge client bound to one
// repository, plus the local repository when the command needs one.
type target struct {
	Client forgeClient
	Bound  jjclient.BoundRepo
	Local  *jjclient.Repo
	Auth   jjclient.Authenticator
}

// RepoName returns the owner/name this target acts on.
func (t target) RepoName() string { return t.Bound.FullName() }

// resolveTarget binds the command's --repo/--instance flags, or the local
// repository's remotes, to a configured forge instance and its client.
//
// The local repository is only opened when one exists, and its absence is not
// an error yet: --repo may supply the binding instead. Commands that need
// local git requireOne calls with requireLocal afterwards.
func resolveTarget(cmd *cobra.Command) (target, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return target{}, err
	}
	cs, err := clients(cfg)
	if err != nil {
		return target{}, err
	}

	repoFlag := flagString(cmd, "repo")
	instFlag := flagString(cmd, "instance")

	if instFlag != "" {
		cs = filterClients(cs, instFlag)
		if len(cs) == 0 {
			return target{}, fmt.Errorf("no configured instance named %q", instFlag)
		}
	}

	// A local repository is only opened when one exists, and its absence is not
	// an error yet: --repo may supply the binding instead.
	var local *jjclient.Repo
	dir := workDir(cmd)
	if jjclient.Exists(dir) {
		opened, err := jjclient.Open(dir)
		if err != nil {
			return target{}, err
		}
		local = opened
	}

	bindOpts := jjclient.BindOptions{Instance: instFlag, Repo: repoFlag}
	if repoFlag == "" && local == nil {
		return target{}, fmt.Errorf("not inside a git repository; run inside a clone or pass --repo owner/name (with --instance when several forges are configured)")
	}

	ctx, err := jjclient.Bind(local, cfg, bindOpts)
	if err != nil {
		return target{}, err
	}

	client, err := clientForInstance(cs, ctx.Bound.Instance)
	if err != nil {
		return target{}, err
	}
	return target{Client: client, Bound: ctx.Bound, Local: local, Auth: ctx.Auth}, nil
}

// requireLocal returns an error when the target has no local repository, which
// the commands touching git objects need.
func (t target) requireLocal() error {
	if t.Local == nil {
		return fmt.Errorf("this command needs a local git repository; run it inside a clone")
	}
	return nil
}

// clientForInstance picks the client matching the bound instance name.
func clientForInstance(cs []forgeClient, instance string) (forgeClient, error) {
	for _, c := range cs {
		if c.Name() == instance {
			return c, nil
		}
	}
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name())
	}
	return nil, fmt.Errorf("no configured instance named %q (configured: %s)", instance, strings.Join(names, ", "))
}

// workDir returns the directory local repository discovery starts from.
func workDir(cmd *cobra.Command) string {
	if d := flagString(cmd, "dir"); d != "" {
		return d
	}
	return "."
}

// parseNumber parses a pull request or issue number argument.
func parseNumber(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid number", arg)
	}
	if n <= 0 {
		return 0, fmt.Errorf("number must be positive, got %d", n)
	}
	return n, nil
}

// bodyFromArgs joins positional arguments into a request body, so that both
// `forgit pr comment 42 "text"` and `forgit pr comment 42 text words` work.
func bodyFromArgs(args []string) string { return strings.Join(args, "\n") }

// withTimeout derives the command context with the standard timeout.
func withTimeout(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cmd.Context(), 60*time.Second)
}

// registerTargetFlags adds the flags every targeted command shares.
func registerTargetFlags(cmd *cobra.Command) {
	cmd.Flags().String("repo", "", "target repository as owner/name (default: inferred from the local repository's remote)")
	cmd.Flags().String("instance", "", "target a specific configured instance")
}

// resolveCapability asserts a client to a capability interface, turning an
// unsupported forge into the standard ErrNotSupported message. Commands render
// that as a note rather than a failure.
func resolveCapability[T any](client forgeClient, feature string) (T, error) {
	if v, ok := as[T](client); ok {
		return v, nil
	}
	var zero T
	return zero, errNoCapability(client.Name(), feature)
}
