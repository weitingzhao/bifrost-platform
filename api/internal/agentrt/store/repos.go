package store

import (
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/infracheck"
)

// Repo is what a goal or a plan needs to know about a repository it may name:
// the commands every implementation task's deterministic check runs on it,
// from the repository root; the Go modules a plan's own checks may test
// (checkTest); and what agentd prepares before an attempt starts: Siblings,
// repositories cloned beside the checkout at a branch's head, and Setup,
// commands run in the checkout with the network a sandboxed executor lacks
// (CUT-agentd-console-checks-2026-10-16). Core are the directories that hold
// the runtime's core, where core work is (CoreText); a repository with none
// has no core work. TestDB: agentd gives the repository's attempts, an
// executor's and a check's, a throwaway Postgres and Redis of their own for
// the tests that need them (agentd testdb.go, TD-403); agentd reads it from
// this table, so the spec carries nothing an older agentd would refuse.
// HandIn are the fast commands an executor runs before it hands in, from the
// repository root: what its sandbox, which cannot open local sockets, can run
// in a minute or two; Verify runs rule tests and changed packages' full tests
// with their databases on the commit, plus Console checks (HandInChecks).
type Repo struct {
	Verify   [][]string
	HandIn   [][]string
	TestDirs []string
	Setup    [][]string
	Siblings []Sibling
	Core     []string
	TestDB   bool
}

// Sibling is a repository beside a task's checkout, at the head of Ref.
type Sibling struct {
	Repo, Ref string
}

// Repos are the repositories goals and plans may name.
//
// Adding a repository is one more entry here, with at least two fields:
// Verify, the commands every implementation task's deterministic check runs
// on the commit, from the repository root; and TestDirs, the module
// directories (the <dir> of go -C <dir> test) where a plan may add its own
// checks, which checkTest in planparse.go enforces. Setup, Siblings, Core and
// TestDB are as Repo says. The defaults are what runs in a fresh agentd
// checkout with agentd's test databases; a plan also proves its own
// change with targeted go test [-run <pattern>] ./<package> checks in TestDirs.
//
// The platform's checks are CI's (ci-platform): the API module built and
// vetted, the module's rule tests and changed Go packages tested with agentd's
// databases, and the Console linted, tested and built. The Console's @bifrost/ui is
// file:../../bifrost-ui, so bifrost-ui's main sits beside the checkout, as CI
// clones it by default, and both are installed before the executor starts.
// The API module's dependencies are downloaded first, into the attempt's
// GOMODCACHE, which its executor shares (agentd's homePins): codex's sandbox
// has no network, and with an empty cache it could not build or vet the
// packages it changed, so it handed in work it had checked only file by file
// (TD-404, PROD M-23).
//
// The infra repository has no Python manifest; its quick check is shared with
// ci-infra and runs Python tests that need only the repository, pytest and
// PyYAML (CUT-runtime-repo-infra-2026-10-19): run by the Python of a virtual
// environment the setup makes in .git/venv, where no commit carries it and a
// fresh clone has none, so neither the change under test nor an earlier
// attempt chooses the interpreter or the packages. pip reads no configuration
// file. pytest reads no configuration file and no conftest.py, the checkout's
// or any directory's above it, and loads no plugin it finds installed, so a
// change cannot make the run collect only or swallow its failures by
// configuration (review 330 A1), nor by a package's __init__.py (infracheck.Check);
// the tests' own code is the review's to read.
// The environment comes first on PATH: the tests start python3 themselves and
// expect pytest and PyYAML there. Go's build cache goes under TMPDIR where
// there is one (an executor's own, in codex's sandbox, which cannot write the
// home directory), else where Go keeps it on Linux.
var Repos = map[string]Repo{
	"bifrost-platform": {
		Verify: [][]string{
			{"go", "-C", "api", "build", "./..."},
			{"go", "-C", "api", "vet", "./..."},
			{"go", "-C", "api", "run", "./cmd/agentrt-check", "rules"},
			{"go", "-C", "api", "run", "./cmd/agentrt-check", "changed"},
			{"npm", "--prefix", "console", "run", "lint"},
			{"npm", "--prefix", "console", "test"},
			{"npm", "--prefix", "console", "run", "build"},
		},
		HandIn: [][]string{
			{"go", "-C", "api", "build", "./..."},
			{"go", "-C", "api", "vet", "./..."},
			{"npm", "--prefix", "console", "run", "lint"},
		},
		TestDirs: []string{"api"},
		Setup: [][]string{
			{"go", "-C", "api", "mod", "download"},
			{"npm", "--prefix", "../bifrost-ui", "ci", "--no-audit", "--no-fund"},
			{"npm", "--prefix", "console", "ci", "--no-audit", "--no-fund"},
		},
		Siblings: []Sibling{{Repo: "bifrost-ui", Ref: "main"}},
		Core:     []string{"api/internal/agentrt", "api/internal/agentd", "api/cmd/agentd"},
		TestDB:   true,
	},
	"bifrost-trade-infra": {
		Verify: [][]string{{"sh", "-c", infracheck.Check()}},
		HandIn: [][]string{{"sh", "-c", infracheck.Check()}},
		Setup:  infracheck.Setup(""),
	},
}

// RepoNames are the names of Repos, sorted.
func RepoNames() []string {
	names := make([]string, 0, len(Repos))
	for n := range Repos {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// SpecTimeoutS is how long an implementation task's check may run, all of
// its commands.
const SpecTimeoutS = 1500
