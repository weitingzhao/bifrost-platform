package store

import (
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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
type Repo struct {
	Verify   [][]string
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
// checkout, not the full test suite, which needs services the workspace lacks
// and would fail for reasons unrelated to the change: a plan proves its own
// change with targeted go test [-run <pattern>] ./<package> checks in TestDirs.
//
// The platform's checks are CI's (ci-platform): the API module built and
// vetted (the full Go suite needs databases agentd's workspace has not), and
// the Console linted, tested and built. The Console's @bifrost/ui is
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
			{"npm", "--prefix", "console", "run", "lint"},
			{"npm", "--prefix", "console", "test"},
			{"npm", "--prefix", "console", "run", "build"},
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
		Setup:  infracheck.Setup(""),
	},
}

// CheckoutOf says what an implementation task in repo works in, as agentd
// prepares it: a fresh checkout of repo at the task's base commit and, beside
// it, the siblings Repos names, each at the head of its ref; nothing else.
func CheckoutOf(repo string) string {
	sibs := Repos[repo].Siblings
	if len(sibs) == 0 {
		return repo + " alone"
	}
	beside := make([]string, 0, len(sibs))
	for _, s := range sibs {
		beside = append(beside, "../"+s.Repo+" at the head of its "+s.Ref)
	}
	return repo + ", with " + strings.Join(beside, " and ") + " beside it"
}

// inCheckout says whether a task in repo has name at ../name: one of its
// siblings.
func inCheckout(repo, name string) bool {
	return slices.ContainsFunc(Repos[repo].Siblings, func(s Sibling) bool { return s.Repo == name })
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
const SpecTimeoutS = 900

// ImplementSpec is the task spec agentd runs for an implementation: what to
// do, the house rules and the checks, as canonical JSON (agentd's ParseSpec),
// with the repository's siblings and setup when it has them. verify holds the
// repository's own checks first; a plan may add more.
func ImplementSpec(title, text string, repo Repo, verify [][]string) ([]byte, error) {
	if len(verify) == 0 {
		return nil, errors.New("store: an implementation without checks")
	}
	var cmds, setup strings.Builder
	for _, c := range verify {
		cmds.WriteString("    " + shellLine(c) + "\n")
	}
	for _, c := range repo.Setup {
		setup.WriteString("    " + shellLine(c) + "\n")
	}
	body := "# " + title + "\n\n" + text + "\n\n---\n" +
		"This task comes from a goal the Owner typed in the Console. How to hand it in:\n\n" +
		"- Change what the task needs in this repository and nothing else.\n" +
		installed(setup.String()) +
		"- Before you hand in, run from the repository root:\n\n" + cmds.String() + "\n" +
		"  The platform runs the same commands on your commit; a failing one sends the work back.\n" +
		"- If the task is unclear or too big for one change, do the part you can and say in your report what is left and why.\n" +
		"- Your report says what you changed, what you ran and how it ended, and what you could not do.\n"
	list := make([]any, 0, len(verify))
	for _, c := range verify {
		args := make([]any, 0, len(c))
		for _, s := range c {
			args = append(args, s)
		}
		list = append(list, args)
	}
	spec := map[string]any{"text": body, "verify": list, "timeout_s": SpecTimeoutS}
	if len(repo.Setup) > 0 {
		cmds := make([]any, 0, len(repo.Setup))
		for _, c := range repo.Setup {
			args := make([]any, 0, len(c))
			for _, s := range c {
				args = append(args, s)
			}
			cmds = append(cmds, args)
		}
		spec["setup"] = cmds
	}
	if len(repo.Siblings) > 0 {
		sibs := make([]any, 0, len(repo.Siblings))
		for _, s := range repo.Siblings {
			sibs = append(sibs, map[string]any{"repo": s.Repo, "ref": s.Ref})
		}
		spec["siblings"] = sibs
	}
	return canon.Encode(spec)
}

// installed is the line that tells an executor its dependencies are in place:
// the setup ran before it started, where it may have no network.
func installed(setup string) string {
	if setup == "" {
		return ""
	}
	return "- Dependencies were installed before you started, by:\n\n" + setup + "\n" +
		"  You may have no network: use what is installed and add no packages.\n"
}

// shellLine is a command as one line a shell without aliases runs as written
// (review 331 B1: an alias still rewrites a first word unquoted): an argument
// with anything but letters, digits and _-./=:@%+, in it is single-quoted, so
// the infra check's sh -c script reads as one argument; so is a first word a
// shell would read as an assignment or a reserved word (review 330 B1).
func shellLine(c []string) string {
	words := make([]string, len(c))
	for i, s := range c {
		if s != "" && strings.TrimFunc(s, shellPlain) == "" && (i > 0 || !strings.Contains(s, "=") && !shellReserved[s]) {
			words[i] = s
		} else {
			words[i] = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return strings.Join(words, " ")
}

// shellPlain is a character a shell reads as part of a word, unquoted.
func shellPlain(r rune) bool {
	return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-./=:@%+,", r))
}

// shellReserved are the words POSIX shells (and bash) read as syntax at the
// start of a command.
var shellReserved = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "fi": true, "do": true, "done": true,
	"case": true, "esac": true, "while": true, "until": true, "for": true, "in": true, "function": true, "select": true, "time": true,
	"coproc": true}
