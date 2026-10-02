package gitlocal

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/pratyay360/forgit/operations"
)

// Authenticator is the credential used for git transport operations.
type Authenticator interface {
	transport.AuthMethod
}

// authFor builds the git credential for an instance. Over HTTP the forge token
// is used as the basic-auth password, which is what GitHub, GitLab, Forgejo and
// Bitbucket expect for API and git-over-HTTPS tokens alike. SSH remotes rely on
// the user's agent or key files instead, since an API token is not an SSH
// credential.
func authFor(inst operations.Instance, remoteURL string) Authenticator {
	if isSSH(remoteURL) {
		return sshAuth()
	}
	if inst.Token == "" {
		return nil
	}
	user := inst.Username
	if user == "" {
		// GitHub accepts any username with the token as password; gitlab and
		// forgejo accept the token as the username with an empty password.
		// Using the token in both positions is accepted by all of them.
		user = inst.Token
	}
	return &http.BasicAuth{Username: user, Password: inst.Token}
}

func sshAuth() Authenticator {
	auth, err := ssh.NewSSHAgentAuth("git")
	if err != nil {
		// No agent available; fall back to the default SSH discovery, which
		// reads ~/.ssh/config and the known identity files.
		return nil
	}
	return auth
}

// isSSH reports whether a remote URL uses SSH.
func isSSH(remoteURL string) bool {
	if len(remoteURL) >= 6 && remoteURL[:6] == "ssh://" {
		return true
	}
	// scp-style git@host:owner/name
	return len(remoteURL) > 0 && !containsScheme(remoteURL) && hasUserAtHost(remoteURL)
}

func containsScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ':':
			return true
		case '/':
			return false
		}
	}
	return false
}

func hasUserAtHost(s string) bool {
	at := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			at = i
			break
		}
		if s[i] == ':' || s[i] == '/' {
			return false
		}
	}
	return at > 0
}

// DescribeAuth returns a short human-readable description of the credential in
// use, for verbose output. It never includes the token itself.
func DescribeAuth(a Authenticator) string {
	if a == nil {
		return "none"
	}
	return a.Name()
}

// authError wraps an authentication failure with a hint about credentials.
func authError(err error, remoteURL string) error {
	if err == transport.ErrAuthenticationRequired {
		if isSSH(remoteURL) {
			return fmt.Errorf("authenticating to %s: no SSH agent or key found; configure an SSH key or use an https remote: %w", remoteURL, err)
		}
		return fmt.Errorf("authenticating to %s: the configured token was rejected; check the token for this instance: %w", remoteURL, err)
	}
	return err
}
