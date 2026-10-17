package store

import (
	"errors"
	"slices"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Repo is what a goal or a plan needs to know about a repository it may name:
// the commands every implementation task's deterministic check runs on it,
// from the repository root; the Go modules a plan's own checks may test
// (checkTest); and what agentd prepares before an attempt starts: Siblings,
// repositories cloned beside the checkout at a branch's head, and Setup,
// commands run in the checkout with the network a sandboxed executor lacks
// (CUT-agentd-console-checks-2026-10-16).
type Repo struct {
	Verify   [][]string
	TestDirs []string
	Setup    [][]string
	Siblings []Sibling
}

// Sibling is a repository beside a task's checkout, at the head of Ref.
type Sibling struct {
	Repo, Ref string
}

// Repos are the repositories goals and plans may name. The platform only so
// far. Its checks are CI's (ci-platform): the API module built and vetted (the
// full Go suite needs databases agentd's workspace has not), and the Console
// linted, tested and built. The Console's @bifrost/ui is file:../../bifrost-ui,
// so bifrost-ui's main sits beside the checkout, as CI clones it by default,
// and both are installed before the executor starts.
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
			{"npm", "--prefix", "../bifrost-ui", "ci", "--no-audit", "--no-fund"},
			{"npm", "--prefix", "console", "ci", "--no-audit", "--no-fund"},
		},
		Siblings: []Sibling{{Repo: "bifrost-ui", Ref: "main"}},
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
		cmds.WriteString("    " + strings.Join(c, " ") + "\n")
	}
	for _, c := range repo.Setup {
		setup.WriteString("    " + strings.Join(c, " ") + "\n")
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
