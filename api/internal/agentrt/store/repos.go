package store

import (
	"errors"
	"slices"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Repo is what a goal or a plan needs to know about a repository it may name:
// the commands every implementation task's deterministic check runs on it,
// from the repository root.
type Repo struct {
	Verify [][]string
}

// Repos are the repositories goals and plans may name. The platform only so
// far; its checks build and vet the API module, which run in any checkout
// (the full test suite needs databases agentd's workspace has not).
var Repos = map[string]Repo{
	"bifrost-platform": {Verify: [][]string{{"go", "-C", "api", "build", "./..."}, {"go", "-C", "api", "vet", "./..."}}},
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
// do, the house rules and the checks, as canonical JSON (agentd's ParseSpec).
// verify holds the repository's own checks first; a plan may add more.
func ImplementSpec(title, text string, verify [][]string) ([]byte, error) {
	if len(verify) == 0 {
		return nil, errors.New("store: an implementation without checks")
	}
	var cmds strings.Builder
	for _, c := range verify {
		cmds.WriteString("    " + strings.Join(c, " ") + "\n")
	}
	body := "# " + title + "\n\n" + text + "\n\n---\n" +
		"This task comes from a goal the Owner typed in the Console. How to hand it in:\n\n" +
		"- Change what the task needs in this repository and nothing else.\n" +
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
	return canon.Encode(map[string]any{"text": body, "verify": list, "timeout_s": SpecTimeoutS})
}
