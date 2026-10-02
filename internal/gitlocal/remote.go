package gitlocal

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/pratyay360/forgit/operations"
)

// BoundRepo identifies the forge repository a local repository tracks.
type BoundRepo struct {
	// Instance is the configured instance name (e.g. "gitlab-work").
	Instance string
	// Forge is the forge type of that instance.
	Forge string
	// Owner is the namespace: user, org, group or workspace.
	Owner string
	// Name is the repository name without namespace.
	Name string
	// Remote is the local remote name the match came from.
	Remote string
}

// FullName returns the owner/name form used by the forge APIs.
func (b BoundRepo) FullName() string {
	if b.Owner == "" {
		return b.Name
	}
	return b.Owner + "/" + b.Name
}

// Context describes the local repository plus the forge repository it tracks.
type Context struct {
	Local *Repo
	Bound BoundRepo
	// Auth is the credential for git operations, or nil when the remote needs
	// none (a local path or a public repository over SSH).
	Auth Authenticator
}

// CandidateHost records that a remote URL pointed at a known forge host but
// did not yield a usable owner/name, so the error can explain the mismatch.
type CandidateHost struct {
	Host  string
	Forge string
}

// BindOptions selects how a local repository is matched to a forge.
type BindOptions struct {
	// Instance, when set, restricts matching to that configured instance.
	Instance string
	// Repo, when set, is an explicit owner/name that skips remote matching.
	Repo string
}

// Bind matches the local repository's remotes against the configured forge
// instances and returns the resolved context.
//
// Matching works on the URL host and the path shape, because every forge spells
// its clone URL differently: GitLab allows nested groups, SourceHut uses
// ~user/name, and Bitbucket is workspace scoped. When Instance or Repo is set
// the remote is not consulted, so the commands work outside a clone.
func Bind(local *Repo, cfg operations.Config, opts BindOptions) (Context, error) {
	instances := cfg.All()

	if opts.Repo != "" {
		inst, err := selectInstance(instances, opts.Instance, opts.Repo)
		if err != nil {
			return Context{}, err
		}
		owner, name := splitRepoName(opts.Repo, inst.Type)
		return Context{
			Local: local,
			Bound: BoundRepo{Instance: inst.Name, Forge: inst.Type, Owner: owner, Name: name},
		}, nil
	}

	if local == nil {
		return Context{}, fmt.Errorf("no local repository: run inside a clone, or pass --repo owner/name")
	}

	remotes, err := local.Remotes()
	if err != nil {
		return Context{}, err
	}
	if len(remotes) == 0 {
		return Context{}, fmt.Errorf("no git remotes configured in %s; add one or pass --repo owner/name with --instance", local.Root())
	}

	// Prefer "origin", then the remaining remotes in config order, so the
	// result does not depend on map iteration order.
	ordered := orderRemotes(remotes)
	var hosts []CandidateHost
	var firstErr error
	for _, rem := range ordered {
		for _, raw := range rem.URLs {
			host := urlHost(raw)
			if host == "" {
				continue // local path or unsupported scheme
			}
			inst, ok := matchInstance(instances, host)
			if !ok {
				continue
			}
			owner, name, ok := parseForgePath(inst.Type, raw)
			if !ok {
				hosts = append(hosts, CandidateHost{Host: host, Forge: inst.Type})
				continue
			}
			return Context{
				Local: local,
				Bound: BoundRepo{Instance: inst.Name, Forge: inst.Type, Owner: owner, Name: name, Remote: rem.Name},
				Auth:  authFor(inst, raw),
			}, nil
		}
	}

	if firstErr != nil {
		return Context{}, firstErr
	}
	return Context{}, explainNoMatch(ordered, hosts, instances)
}

// explainNoMatch turns the failed matching into an actionable error listing
// the remotes that were inspected.
func explainNoMatch(remotes []Remote, hosts []CandidateHost, instances []operations.Instance) error {
	if len(hosts) > 0 {
		// The host matched a configured forge, so the shape was the problem.
		h := hosts[0]
		return fmt.Errorf("remote points at %s (%s) but its URL did not contain a repository path; pass --repo owner/name (and --instance %s) to name it explicitly", h.Host, h.Forge, instanceOfType(instances, h.Forge))
	}
	names := make([]string, 0, len(remotes))
	for _, r := range remotes {
		names = append(names, fmt.Sprintf("%s=%s", r.Name, r.URL()))
	}
	return fmt.Errorf("no configured forge matches any remote (%s); configure the forge instance, or pass --repo owner/name with --instance", strings.Join(names, ", "))
}

func instanceOfType(instances []operations.Instance, forge string) string {
	for _, i := range instances {
		if i.Type == forge {
			return i.Name
		}
	}
	return forge
}

// selectInstance picks the instance for an explicit --repo. Without --instance
// it infers the forge from the URL of the instance's host, defaulting to the
// single configured instance when only one exists.
func selectInstance(instances []operations.Instance, want, repo string) (operations.Instance, error) {
	if want != "" {
		for _, i := range instances {
			if i.Name == want {
				return i, nil
			}
		}
		return operations.Instance{}, fmt.Errorf("no configured instance named %q (configured: %s)", want, instanceNames(instances))
	}
	if len(instances) == 1 {
		return instances[0], nil
	}
	// Try to infer from a host appearing in the instance URL; ambiguous setups
	// ask for --instance rather than guessing.
	for _, i := range instances {
		if i.URL == "" {
			continue
		}
		if h := urlHost(i.URL); h != "" && strings.Contains(repo, h) {
			return i, nil
		}
	}
	return operations.Instance{}, fmt.Errorf("several forges are configured (%s); pass --instance to choose one", instanceNames(instances))
}

func instanceNames(instances []operations.Instance) string {
	names := make([]string, 0, len(instances))
	for _, i := range instances {
		names = append(names, i.Name)
	}
	return strings.Join(names, ", ")
}

// orderRemotes puts "origin" first and keeps the configured order after it.
func orderRemotes(remotes []Remote) []Remote {
	out := make([]Remote, 0, len(remotes))
	for _, r := range remotes {
		if r.Name == "origin" {
			out = append(out, r)
		}
	}
	for _, r := range remotes {
		if r.Name != "origin" {
			out = append(out, r)
		}
	}
	return out
}

// urlHost extracts the lowercase host from an http(s), ssh or git remote URL.
// It returns "" for local paths and unsupported schemes.
func urlHost(raw string) string {
	if raw == "" {
		return ""
	}
	// scp-style syntax, e.g. git@github.com:owner/name.git. The host is the
	// portion between the optional user@ and the literal :.
	if !strings.Contains(raw, "://") {
		hostPart := raw
		if i := strings.Index(hostPart, ":"); i > 0 && !strings.Contains(hostPart[:i], "/") {
			hostPart = hostPart[:i]
		}
		if at := strings.LastIndex(hostPart, "@"); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		if hostPart == "" || strings.ContainsAny(hostPart, "/ ") {
			return ""
		}
		return strings.ToLower(hostPart)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	switch u.Scheme {
	case "http", "https", "ssh", "git":
		return strings.ToLower(u.Hostname())
	}
	return ""
}

// matchInstance finds the configured instance whose host appears in host. A
// host-less instance (GitHub.com, SourceHut) matches its known default hosts.
func matchInstance(instances []operations.Instance, host string) (operations.Instance, bool) {
	for _, i := range instances {
		if h := instanceHost(i); h != "" && h == host {
			return i, true
		}
	}
	return operations.Instance{}, false
}

// defaultHosts lists the well-known hosts per forge type, used when an
// instance does not pin a URL of its own.
var defaultHosts = map[string][]string{
	"github":    {"github.com"},
	"gitlab":    {"gitlab.com"},
	"forgejo":   {"codeberg.org", "codeberg.com"},
	"sourcehut": {"git.sr.ht", "sr.ht"},
	"bitbucket": {"bitbucket.org"},
}

// instanceHost returns the host an instance is served from.
func instanceHost(i operations.Instance) string {
	if h := urlHost(i.URL); h != "" {
		return h
	}
	hosts := defaultHosts[i.Type]
	if len(hosts) > 0 {
		return hosts[0]
	}
	return ""
}

// parseForgePath extracts the owner and repository name from a clone URL,
// honouring each forge's path conventions. It reports false when the URL does
// not contain both parts.
func parseForgePath(forge, raw string) (owner, name string, ok bool) {
	path := urlPath(raw)
	if path == "" {
		return "", "", false
	}
	segments := splitPath(path)
	if len(segments) == 0 {
		return "", "", false
	}
	last := strings.TrimSuffix(segments[len(segments)-1], ".git")
	if last == "" {
		return "", "", false
	}

	switch forge {
	case "gitlab":
		// Nested groups: any number of leading segments are the namespace.
		if len(segments) < 2 {
			return "", "", false
		}
		return strings.Join(segments[:len(segments)-1], "/"), last, true
	case "sourcehut":
		// SourceHut repos are ~user/name, but the tilde is dropped by scp-style
		// clone URLs, so both spellings appear in the wild.
		if len(segments) < 2 {
			return "", "", false
		}
		owner = strings.TrimPrefix(segments[len(segments)-2], "~")
		return owner, last, true
	default:
		// GitHub, Forgejo and Bitbucket are all owner/name.
		if len(segments) < 2 {
			return "", "", false
		}
		return segments[len(segments)-2], last, true
	}
}

var scpPathRe = regexp.MustCompile(`^([^/:]+):(.+)$`)

// urlPath returns the path component of a remote URL, handling both the URL
// form and the scp-like form used for SSH remotes.
func urlPath(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		if m := scpPathRe.FindStringSubmatch(raw); m != nil {
			raw = "ssh://" + m[1] + "/" + m[2]
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

// splitPath splits a URL path into non-empty segments.
func splitPath(p string) []string {
	var out []string
	for _, s := range strings.Split(strings.Trim(p, "/"), "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// splitRepoName splits an explicit owner/name argument. Forges whose name is
// always the last segment keep nested groups (owner is everything before it).
func splitRepoName(repo, forge string) (owner, name string) {
	segments := splitPath(repo)
	if len(segments) < 2 {
		return "", repo
	}
	return strings.Join(segments[:len(segments)-1], "/"), segments[len(segments)-1]
}
