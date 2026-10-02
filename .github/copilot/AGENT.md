[docs](https://gitforge-api-mcp.netlify.app/mcp)

[golang](https://lobehub.com/skills/jeffallan-claude-skills-golang-pro/skill.md)

[apidocs](https://forgit-api.pages.dev)


# GOAL

A UnIFIED CLI tool to manage multiple forges — GitHub, GitLab, Forgejo,
SourceHut and Bitbucket — from a single binary called `forgit`.


# SCOPE

  1. list / create / rename / delete repositories, change visibility
  2. list / view / create / comment / close issues
  3. list / view / create / comment / close / merge / ready / draft pull requests
  4. check out a remote pull request head into the local git repository
  5. list CI runs and workflow definitions, list project boards
  6. browse repos / issues / pull requests interactively

And every operation must work uniformly across forges, with forges that lack
a concept downgrading to a clear note rather than failing.


# ENVIRONMENT

Tokens are taken from a TOML config file, falling back to environment
variables. The config file lives at `~/.config/forgit/config.toml` by
default; the location can be overridden with `--config <path>` or the
`FORGIT_CONFIG` environment variable.

Per-forge environment variables:

  FORGIT_GITHUB_TOKEN=
  FORGIT_GITLAB_TOKEN=
  FORGIT_GITLAB_URL=           (optional, for self-hosted)
  FORGIT_FORGEJO_TOKEN=
  FORGIT_FORGEJO_URL=          (optional, for self-hosted)
  FORGIT_SOURCEHUT_TOKEN=
  FORGIT_BITBUCKET_TOKEN=
  FORGIT_BITBUCKET_USERNAME=   (required for the bitbucket client)

Multiple instances of the same forge are supported via the [[instance]]
table in the config file, each entry setting a unique `name` and `token`.

Tokens containing shell metacharacters are evaluated through `/bin/sh -c`
so they can pull from `pass`, `gopass`, `op`, `secret-tool` or any other
credential store.


# ADDING A NEW FORGE

  1. Create `operations/<forge>/client.go` implementing `forgeClient` plus
     whatever capability interfaces the forge supports
     (prReader, prWriter, prChecker, prDiffer, prFetcher, issueReader,
     issueWriter).
  2. Add the new forge's `Type` and config struct to `operations/config.go`.
  3. Add a `case` for the new type to `cmd/client.go#clients`.

The rest of the CLI stays unchanged.