package jjclient

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/pratyay360/forgit/operations"
)

// BoundRepo identifies the forge repository a local repository tracks.
type BoundRepo struct {
	Instance string
	Forge    string
	Owner    string
	Name     string
	Remote   string
}

// FullName returns the owner/name form used by the forge APIs.
func (b BoundRepo) FullName() string {
	if b.Owner == "" {
		return b.Name
	}
	return b.Owner + "/" + b.Name
}

// BindOptions selects how a local repository is matched to a forge.
type BindOptions struct {
	Instance string
	Repo     string
}

// BindResult is the return value of Bind.
type BindResult struct {
	Bound BoundRepo
	Auth  Authenticator
}

// Bind matches the local repository's remotes against the configured forge
// instances and returns the resolved context.
func Bind(local *Repo, cfg operations.Config, opts BindOptions) (BindResult, error) {
	instances := cfg.All()

	if opts.Repo != "" {
		inst, err := selectInstance(instances, opts.Instance, opts.Repo)
		if err != nil {
			return BindResult{}, err
		}
		owner, name := splitRepoName(opts.Repo, inst.Type)
		return BindResult{
			Bound: BoundRepo{Instance: inst.Name, Forge: inst.Type, Owner: owner, Name: name},
		}, nil
	}

	if local == nil {
		return BindResult{}, fmt.Errorf("no local repository: run inside a clone, or pass --repo owner/name")
	}

	remotes, err := local.Remotes()
	if err != nil {
		return BindResult{}, err
	}
	if len(remotes) == 0 {
		return BindResult{}, fmt.Errorf("no git remotes configured in %s; add one or pass --repo owner/name with --instance", local.Root())
	}

	ordered := orderRemotes(remotes)
	var hosts []CandidateHost
	var firstErr error
	for _, rem := range ordered {
		for _, raw := range rem.URLs {
			host := urlHost(raw)
			if host == "" {
				continue
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
			return BindResult{
				Bound: BoundRepo{Instance: inst.Name, Forge: inst.Type, Owner: owner, Name: name, Remote: rem.Name},
				Auth:  authFor(inst, raw),
			}, nil
		}
	}

	if firstErr != nil {
		return BindResult{}, firstErr
	}
	return BindResult{}, explainNoMatch(ordered, hosts, instances)
}

// CandidateHost records that a remote URL pointed at a known forge host but
// did not yield a usable owner/name.
type CandidateHost struct {
	Host  string
	Forge string
}

func explainNoMatch(remotes []Remote, hosts []CandidateHost, instances []operations.Instance) error {
	if len(hosts) > 0 {
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

func urlHost(raw string) string {
	if raw == "" {
		return ""
	}
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

var defaultHosts = map[string][]string{
	"github":    {"github.com"},
	"gitlab":    {"gitlab.com"},
	"forgejo":   {"codeberg.org", "codeberg.com"},
	"sourcehut": {"git.sr.ht", "sr.ht"},
	"bitbucket": {"bitbucket.org"},
}

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
		if len(segments) < 2 {
			return "", "", false
		}
		return strings.Join(segments[:len(segments)-1], "/"), last, true
	case "sourcehut":
		if len(segments) < 2 {
			return "", "", false
		}
		owner = strings.TrimPrefix(segments[len(segments)-2], "~")
		return owner, last, true
	default:
		if len(segments) < 2 {
			return "", "", false
		}
		return segments[len(segments)-2], last, true
	}
}

var scpPathRe = regexp.MustCompile(`^([^/:]+):(.+)$`)

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

func splitPath(p string) []string {
	var out []string
	for _, s := range strings.Split(strings.Trim(p, "/"), "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitRepoName(repo, forge string) (owner, name string) {
	segments := splitPath(repo)
	if len(segments) < 2 {
		return "", repo
	}
	return strings.Join(segments[:len(segments)-1], "/"), segments[len(segments)-1]
}

func matchInstance(instances []operations.Instance, host string) (operations.Instance, bool) {
	for _, i := range instances {
		if h := instanceHost(i); h != "" && h == host {
			return i, true
		}
	}
	return operations.Instance{}, false
}