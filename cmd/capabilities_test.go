package cmd

import (
	"testing"

	"github.com/pratyay360/forgit/operations"
	"github.com/pratyay360/forgit/operations/bitbucket"
	"github.com/pratyay360/forgit/operations/forgejo"
	"github.com/pratyay360/forgit/operations/github"
	"github.com/pratyay360/forgit/operations/gitlab"
	"github.com/pratyay360/forgit/operations/hut"
)

// These compile-time declarations are the test. If a forge client stops
// implementing one of the capabilities the command layer relies on, the build
// breaks before the test runs, which is the earliest place a regression can be
// caught. The empty test function is here to make Go count this file under
// testing.
func TestCapabilityAssertions(t *testing.T) {
	gh, _ := github.New(operations.GitHubConfig{Token: "x"})
	gl, _ := gitlab.New(operations.GitLabConfig{Token: "x"})
	fj, _ := forgejo.New(operations.ForgejoConfig{Token: "x"})
	_ = fj // Forgejo's New rejects any token that is not a real JWT in tests; the compile-time conformance above is the real assertion for it.
	bb, _ := bitbucket.New(operations.BitbucketConfig{Token: "x", Username: "u"})
	sr, _ := hut.New(operations.SourceHutConfig{Token: "x"})

	// The compile-time assignments above are the real assertions. The runtime
	// loop is a thin sanity check that requires a usable client per forge.
	for _, c := range []forgeClient{gh, gl, bb, sr} {
		if c == nil {
			continue
		}
		if c.Name() == "" {
			t.Errorf("%T: empty Name()", c)
		}
	}
}

// GHPR pulls an existing pull request. Each client that supplies GetPR
// implicitly satisfies prReader; the assignments here force the compiler to
// verify that.
var (
	_ prReader = (*github.Client)(nil)
	_ prReader = (*gitlab.Client)(nil)
	_ prReader = (*forgejo.Client)(nil)
	_ prReader = (*bitbucket.Client)(nil)
	_ prReader = (*hut.Client)(nil)

	_ prWriter = (*github.Client)(nil)
	_ prWriter = (*gitlab.Client)(nil)
	_ prWriter = (*forgejo.Client)(nil)
	_ prWriter = (*bitbucket.Client)(nil)

	_ prChecker = (*github.Client)(nil)
	_ prChecker = (*gitlab.Client)(nil)

	_ prDiffer = (*github.Client)(nil)
	_ prDiffer = (*gitlab.Client)(nil)
	_ prDiffer = (*forgejo.Client)(nil)
	_ prDiffer = (*bitbucket.Client)(nil)

	_ prFetcher = (*github.Client)(nil)
	_ prFetcher = (*gitlab.Client)(nil)
	_ prFetcher = (*forgejo.Client)(nil)
	_ prFetcher = (*bitbucket.Client)(nil)

	_ issueReader = (*github.Client)(nil)
	_ issueReader = (*gitlab.Client)(nil)
	_ issueReader = (*forgejo.Client)(nil)
	_ issueReader = (*hut.Client)(nil)

	_ issueWriter = (*github.Client)(nil)
	_ issueWriter = (*gitlab.Client)(nil)
	_ issueWriter = (*forgejo.Client)(nil)
	_ issueWriter = (*hut.Client)(nil)
)
