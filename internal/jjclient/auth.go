package jjclient

import (
	"github.com/pratyay360/forgit/operations"
)

type Authenticator interface {
	Name() string
}

func authFor(inst operations.Instance, remoteURL string) Authenticator {
	if isSSH(remoteURL) {
		return nil
	}
	if inst.Token == "" {
		return nil
	}
	return &basicAuth{username: inst.Username, token: inst.Token}
}

type basicAuth struct {
	username string
	token    string
}

func (a *basicAuth) Name() string { return "basic" }

func isSSH(remoteURL string) bool {
	if len(remoteURL) >= 6 && remoteURL[:6] == "ssh://" {
		return true
	}
	return len(remoteURL) > 0 && !containsScheme(remoteURL) && hasUserAtHost(remoteURL)
}

func containsScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
		if s[i] == '/' {
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
