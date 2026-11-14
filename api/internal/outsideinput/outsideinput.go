// Package outsideinput lets a package's tests name the files they read outside
// the Go module (api/), so that Go's test cache reruns them when those files
// change.
//
// Why (2026-11-13, CI feedback 18): go test keeps a passed result and reuses it
// while the test's inputs are unchanged, but it only looks again at files inside
// the module root. CI shares one Go build cache (CI-7). Commit 5f1e5df2 deleted
// Console files that two Go tests read (agentrt/rules stats them, agentrt/store
// compared a Console fixture); its whole-suite CI run ci-platform-4glhs printed
// "(cached)" for both packages and was green, and main stayed red for 28
// minutes. A probe showed the same on a two-package module: a deleted file
// outside the module leaves "ok (cached)", one inside fails.
//
// The fix uses what the cache does look at: an environment variable a test
// reads is part of its key. CI (scripts/ci/outside_inputs.sh) exports one
// variable per input, holding the git object id of that tree or file; a package
// whose tests read such an input calls Declare with its name. With the
// variables unset (a developer's shell) nothing changes.
package outsideinput

import (
	"os"
	"strings"
)

// TB is the part of testing.TB Declare uses.
type TB interface {
	Helper()
	Logf(format string, args ...any)
}

// Env is the variable that holds the object id of one input.
//
//	"console"                       a top-level entry of this repository
//	"."                             everything in this repository outside api/
//	"bifrost-trade-infra:k8s/cicd"  a path, at most two elements deep, in a repository checked out beside this one
//	"bifrost-trade-infra:"          that whole repository
func Env(name string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z':
				return r - 'a' + 'A'
			case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				return r
			}
			return '_'
		}, s)
	}
	if name == "." {
		return "BIFROST_INPUT_ROOT"
	}
	if repo, path, ok := strings.Cut(name, ":"); ok {
		if path == "" {
			return "BIFROST_INPUT_" + clean(repo) + "__"
		}
		return "BIFROST_INPUT_" + clean(repo) + "__" + clean(path)
	}
	return "BIFROST_INPUT_" + clean(name)
}

// Declare reads the variable of each input, which makes its value part of the
// calling test's cache key. Call it from one test of every package whose tests
// read those files.
func Declare(t TB, names ...string) {
	t.Helper()
	for _, name := range names {
		key := Env(name)
		t.Logf("outside input %s: %s=%q", name, key, os.Getenv(key))
	}
}
