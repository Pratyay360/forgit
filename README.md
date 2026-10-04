# forgit

A unified command-line client for GitHub, GitLab, Forgejo, SourceHut and
Bitbucket, in the spirit of `gh` and `glab`, with one config file aggregating
every forge you use.

## What it does

- **List and view** repositories, issues, pull requests (merge requests,
  patchsets), CI runs, workflow definitions and project boards across every
  configured forge.
- **Manage repositories** uniformly on every forge: create, rename, delete,
  change visibility.
- **Work with pull requests**: view, create, comment, close, mark ready for
  review or draft, merge — across every forge with a compatible concept.
- **Work with issues**: list, view, create, comment, close.
- **Check out pull requests from any forge into a local git repository.** A
  pull request head is fetched and a branch is created locally so the change
  can be tested, built or reviewed on disk:

  ```sh
  # inside a clone, the local remote's forge is auto-detected
  forgit pr checkout 1234

  # against an arbitrary repository, no clone needed at all
  forgit --config ./forgit.toml pr --instance github-work --repo alice/api \
      checkout 1234
  ```

  `forgit pr checkout` works against:
  - GitHub (uses `refs/pull/N/head`)
  - GitLab (uses `refs/merge-requests/N/head`)
  - Forgejo (uses `refs/pull/N/head`)
  - Bitbucket (resolves the head commit and fetches the head branch directly;
    for fork pull requests it adds a transient git remote pointing at the
    fork)
  - SourceHut patchsets are delivered as mail series, so there is no review
    ref to fetch — `forgit pr checkout` reports the recommended manual
    alternative.

- **Local git operations**: `forgit git status`, `log`, `diff`, `branch`,
  `remote`. Pull and push are deliberately left to git itself so hooks,
  LFS and SSH credentials keep working out of the box.

## Install

```sh
go install github.com/pratyay360/forgit@latest
```

The result is a binary called `forgit` in your `$GOBIN`.

Other marketplaces:

- macOS (Homebrew): `brew install --cask Pratyay360/tap/forgit`
- Windows (Scoop): `scoop bucket add forgit https://github.com/Pratyay360/scoop-bucket && scoop install forgit`
- Windows (winget): `winget install Pratyay360.forgit`
- Arch Linux (AUR): `yay -S forgit-bin` (or your favourite AUR helper)
- conda: `conda install -c conda-forge forgit`
- Debian/Ubuntu, Fedora/RHEL, Alpine: `.deb`, `.rpm` and `.apk` packages are
  attached to every GitHub release.

## Configure

Create `~/.config/forgit/config.toml`:

```toml
[github]
token = "ghp_..."

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
username = "you"  # required, app password

# Additional instances of any forge type (e.g. a second GitLab server):
[[instance]]
name = "gitlab-work"
type = "gitlab"
token = "..."
url = "https://gitlab.example.com"

# A user-named alias of a built-in driver. The 'type' is the label you see
# everywhere; 'driver' picks which API client handles requests. The URL
# shape must match the driver's (e.g. Gitea uses Forgejo's /api/v1). This
# is the right tool when a server you use is API-compatible with one of
# the built-in forges but you'd rather not label it as that forge — Gitea,
# a self-hosted Gitea/Forgejo mirror, or a server at a non-default URL that
# you want to name distinctly. 'url' is required so the local remote can
# be matched back to this instance.
[[instance]]
name = "gitea-home"
type = "gitea"
driver = "forgejo"
token = "..."
url = "https://gitea.example.com"
```

Or use environment variables, all prefixed `FORGIT_`:

- `FORGIT_CONFIG` — path to the config file
- `FORGIT_GITHUB_TOKEN`, `FORGIT_GITLAB_TOKEN`, `FORGIT_GITLAB_URL`,
  `FORGIT_FORGEJO_TOKEN`, `FORGIT_FORGEJO_URL`,
  `FORGIT_SOURCEHUT_TOKEN`,
  `FORGIT_BITBUCKET_TOKEN`, `FORGIT_BITBUCKET_USERNAME`

Tokens containing shell metacharacters are evaluated through `/bin/sh -c`,
which lets you fetch them from `pass`, `gopass`, `op`, `secret-tool` or any
other credential store.

Run `forgit config` to see the resolved path and which forges are configured.

## Command reference

- `forgit repos`, `forgit issues`, `forgit prs`, `forgit runs`,
  `forgit workflows`, `forgit projects` — cross-forge listings.
- `forgit repo create|rename|delete|visibility` — repository lifecycle.
- `forgit pr list|checkout|view|create|comment|close|ready|draft|merge|diff|web`.
- `forgit issue list|view|create|comment|close`.
- `forgit git status|log|diff|branch|remote`.
- `forgit browse` — interactive TUI for repos, issues, and pull requests.
- `forgit config` — show config path and which forges are configured.

Every targeted command accepts `--repo owner/name` and `--instance NAME` to
act on a forge repository without being inside a clone.

## Implementation notes

- Pure-Go, single binary. No `git` binary required for local git operations
  (uses `go-git`), so cross-platform behaviour is consistent.
- Forges whose pull request model differs from GitHub's degrade gracefully:
  missing capabilities surface as informational notes rather than failures.
- A forge that does not exist yet is one new file in `operations/<forge>/`
  plus one `case` in `cmd/client.go`. The rest of the CLI stays unchanged.
- Forges that are API-compatible with one of the built-in drivers (Gitea,
  internal Forgejo mirrors, etc.) can be added as a user-named alias in
  config: set `driver` on an `[[instance]]` entry and pick whatever
  `type` you want to see in listings.