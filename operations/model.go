// Package operations defines the unified data model and configuration shared
// by the forge-specific clients and the forge CLI commands.
package operations

import "errors"

// ErrIssuesUnsupported is returned by a forge whose issue tracker is not
// available. Bitbucket retired its issue tracker in 2023 and its issue API
// endpoints are deprecated, so the Bitbucket client returns this sentinel.
// Commands surface it as an informational note rather than a failure.
var ErrIssuesUnsupported = errors.New("issue tracking not supported")

// ErrNotSupported is returned by a forge that does not offer a feature the
// unified CLI surface supports elsewhere. Commands surface it as a note
// rather than a failure, keeping the CLI uniform across forges even when a
// particular forge has no equivalent concept or API.
var ErrNotSupported = errors.New("not supported by this forge")

// Repo is a git repository hosted on one of the supported forges.
type Repo struct {
	Forge    string // forge type: github, gitlab, forgejo, sourcehut, bitbucket
	Instance string // instance name (e.g. "gitlab-main"); forge type when unnamed
	FullName string // owner/name
	URL      string // web URL
	Private  bool
}

// RepoInput describes a repository to create.
type RepoInput struct {
	Name        string // required
	Owner       string // optional; defaults to the authenticated user
	Description string
	Private     bool
}

// Issue is a tracker item hosted on one of the supported forges.
type Issue struct {
	Forge    string // forge type
	Instance string // instance name
	Repo     string // repository (or tracker) name
	Number   int
	Title    string
	State    string
	URL      string
}

// PR is a pull request, merge request or patchset on one of the forges.
type PR struct {
	Forge    string // forge type
	Instance string // instance name
	Repo     string // repository (or mailing list) name
	Number   int
	Title    string
	State    string
	URL      string
}

// Run is a CI run, pipeline or build job on one of the forges.
type Run struct {
	Forge    string // forge type
	Instance string // instance name
	Repo     string // repository name
	ID       int64
	Name     string
	Status   string
	Branch   string
	URL      string
}

// Workflow is a CI workflow definition (GitHub Actions workflow) on a repo.
type Workflow struct {
	Forge    string // forge type
	Instance string // instance name
	Repo     string // repository name
	ID       int64
	Name     string
	State    string // e.g. active, disabled
	URL      string
}

// Project is a project or board on one of the forges.
type Project struct {
	Forge    string // forge type
	Instance string // instance name
	FullName string // name or owner/name
	URL      string
	Private  bool
}
