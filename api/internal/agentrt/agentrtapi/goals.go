package agentrtapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Goals (W-31 2.2b; the Owner, 2026-10-15): the Console's one-sentence entry.
// A goal goes to the chief of staff's seat first (the Owner's D1 A): it
// becomes a mission, inside the standing envelope when it fits, and one
// planning task on the head of the repository's main, in one transaction
// under the caller's key (store.ProposeGoal). The planner splits it into
// implementation tasks with their checks, asks the Owner only what is his and
// records its choices; the controller checks the plan and records it
// (store.AcceptPlan).

// MainHead reads the commit the main branch of a repository points at, where
// agentd clones repositories from.
type MainHead func(ctx context.Context, repo string) (string, error)

// GoalRepo and GoalRepos are the repositories a goal may name and their
// checks (store.Repos).
type GoalRepo = store.Repo

var GoalRepos = store.Repos

// GoalSuccess is a goal's mission's success criterion until its plan names
// its own.
const GoalSuccess = store.GoalSuccess

const (
	maxGoalRunes  = 2000 // what one sentence (or a short paragraph) needs
	maxTitleRunes = 80
)

// goalBody is POST /goals: the goal and its repositories, as repos (the
// Owner 10-19: a goal may name several) or, as before, one repo; never both,
// whatever either holds, null or empty included (review 332 B1): the fields
// are kept as they came, so that one given is told from one left out.
type goalBody struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Goal           string          `json:"goal"`
	Repo           json.RawMessage `json:"repo"`
	Repos          json.RawMessage `json:"repos"`
}

// proposeGoal is POST /goals. A key with an answer gets it; a new key reads
// main and writes the mission, the planning task and its spec together, so a
// retry gets the first try's answer (its base among it), even while main
// cannot be read, and no try leaves half a goal behind.
func (a *API) proposeGoal(w http.ResponseWriter, r *http.Request) {
	var b goalBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	goal, repos, err := checkGoal(b)
	if err != nil {
		a.fail(w, err)
		return
	}
	if a.MainHead == nil {
		writeError(w, http.StatusServiceUnavailable, "no_main_reader", "the platform has no reader for the repositories' main branch")
		return
	}
	out, err := store.ProposeGoal(r.Context(), a.DB, who, b.IdempotencyKey, store.Goal{Title: goalTitle(goal), Text: goal, Repos: repos,
		Main: a.MainHead})
	if errors.Is(err, store.ErrNoMain) {
		a.logf("agentrt goals: %v", err)
		writeError(w, http.StatusServiceUnavailable, "no_main", fmt.Sprintf("the main branch of %s cannot be read now; try again", strings.Join(repos, " or ")))
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// checkGoal trims the goal and refuses what is not one: empty, too long, a
// control character other than a line break or a tab, a repository a goal
// may not name, repo and repos both or neither, no idempotency key. The
// repositories come back sorted (store.CheckGoalRepos).
func checkGoal(b goalBody) (goal string, repos []string, err error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", store.ErrInvalid, fmt.Sprintf(format, args...))
	}
	if b.IdempotencyKey == "" {
		return "", nil, invalid("an idempotency key is required")
	}
	goal = strings.TrimSpace(b.Goal)
	switch n := utf8.RuneCountInString(goal); {
	case !utf8.ValidString(goal):
		return "", nil, invalid("the goal is not UTF-8")
	case n == 0:
		return "", nil, invalid("a goal says what to do")
	case n > maxGoalRunes:
		return "", nil, invalid("a goal is at most %d characters; this one has %d", maxGoalRunes, n)
	}
	for _, c := range goal {
		if unicode.IsControl(c) && c != '\n' && c != '\t' {
			return "", nil, invalid("the goal has a control character %U", c)
		}
	}
	if b.Repo != nil && b.Repos != nil {
		return "", nil, invalid("a goal names repo or repos, not both")
	}
	if b.Repos == nil {
		var repo string
		if b.Repo != nil && json.Unmarshal(b.Repo, &repo) != nil {
			return "", nil, invalid("repo is a repository's name")
		}
		if _, ok := GoalRepos[repo]; !ok {
			return "", nil, invalid("a goal may name %s, not %q", strings.Join(store.RepoNames(), ", "), repo)
		}
		return goal, []string{repo}, nil
	}
	var names []string
	if json.Unmarshal(b.Repos, &names) != nil {
		return "", nil, invalid("repos is a list of repositories' names")
	}
	if repos, err = store.CheckGoalRepos(names); err != nil {
		return "", nil, err
	}
	return goal, repos, nil
}

// goalTitle is the goal's first line, cut to maxTitleRunes.
func goalTitle(goal string) string {
	line, _, _ := strings.Cut(goal, "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) <= maxTitleRunes {
		return line
	}
	return string([]rune(line)[:maxTitleRunes-1]) + "…"
}
