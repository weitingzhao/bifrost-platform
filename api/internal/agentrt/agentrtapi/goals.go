package agentrtapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Goals (W-31 2.2b, the first slice; the Owner, 2026-10-15): the Console's
// one-sentence entry. A goal becomes a mission, inside the standing envelope
// when it fits, with one implementation task: its spec is the goal and the
// house rules below, its checks the repository's default commands, its base
// the head of the repository's main where agentd clones it. Splitting a goal
// into several tasks is the planner's (2.4), not this route's.

// MainHead reads the commit the main branch of a repository points at, where
// agentd clones repositories from.
type MainHead func(ctx context.Context, repo string) (string, error)

// GoalRepo is what a goal needs to know about a repository it may name: the
// commands its task's deterministic check runs, from the repository root.
type GoalRepo struct {
	Verify [][]string
}

// GoalRepos are the repositories a goal may name. The first slice has the
// platform only; its checks build and vet the API module, which run in any
// checkout (the full test suite needs databases agentd's workspace has not).
var GoalRepos = map[string]GoalRepo{
	"bifrost-platform": {Verify: [][]string{{"go", "-C", "api", "build", "./..."}, {"go", "-C", "api", "vet", "./..."}}},
}

// GoalSuccess is a goal's mission's success criterion.
const GoalSuccess = "the implementation task is accepted: its checks pass and a reviewer of another provider passes it"

const (
	maxGoalRunes  = 2000 // what one sentence (or a short paragraph) needs
	maxTitleRunes = 80
	goalTimeoutS  = 900 // the check's commands, all of them
)

type goalBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	Goal           string `json:"goal"`
	Repo           string `json:"repo"`
}

// proposeGoal is POST /goals. It uploads the spec, proposes the mission and
// proposes the task, the two proposals under keys derived from the caller's,
// so a retry after any step finishes what is left and answers the same ids.
// The base is main as read on each try: a retry after a try that stopped
// before the task was proposed puts the task on main as it is then; once the
// task was proposed, a retry after main moved is another task request under
// the same key and is refused as a reused key (409).
//
// The spec goes first and must be stored as a task spec: an artifact is kept
// by its bytes, so the same bytes uploaded before as another kind (a report)
// would let the mission be proposed and every try of its task be refused,
// leaving a mission no retry can finish (review 213 A1). Such a goal is
// refused before anything is proposed.
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
	goal, repo, err := checkGoal(b)
	if err != nil {
		a.fail(w, err)
		return
	}
	if a.MainHead == nil {
		writeError(w, http.StatusServiceUnavailable, "no_main_reader", "the platform has no reader for the repositories' main branch")
		return
	}
	base, err := a.MainHead(r.Context(), repo)
	if err != nil {
		a.logf("agentrt goals: main of %s: %v", repo, err)
		writeError(w, http.StatusServiceUnavailable, "no_main", fmt.Sprintf("the main branch of %s cannot be read now; try again", repo))
		return
	}
	title := goalTitle(goal)
	spec, err := goalSpec(title, goal, GoalRepos[repo])
	if err != nil {
		a.fail(w, err)
		return
	}
	specID, _, err := store.UploadArtifact(r.Context(), a.DB, who, "task_spec", "application/json", spec)
	if err != nil {
		a.fail(w, err)
		return
	}
	var kind string
	if err = store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT artifact_kind FROM agentrt.artifact WHERE artifact_id = $1`, specID).Scan(&kind)
	}); err != nil {
		a.fail(w, err)
		return
	}
	if kind != "task_spec" {
		writeError(w, http.StatusConflict, "spec_kind", fmt.Sprintf(
			"this goal's spec is already stored as %s, not as a task spec; word the goal differently", kind))
		return
	}
	m, err := store.ProposeMission(r.Context(), a.DB, who, "goal:"+b.IdempotencyKey+":mission", store.MissionProposal{
		Title: title, Goal: goal, Repos: []string{repo}, Success: []string{GoalSuccess}})
	if err != nil {
		a.fail(w, err)
		return
	}
	mission, _ := m["mission_id"].(string)
	t, err := store.ProposeTasks(r.Context(), a.DB, who, "goal:"+b.IdempotencyKey+":task", mission, []store.TaskProposal{{
		Ref: "goal", OffPlanReason: "a goal from the Console; the runtime has no plan tree yet", Role: "implement",
		Class: "goal", Title: title, SpecArtifact: specID, Effect: "branch", Repo: repo, BaseCommit: base}})
	if err != nil {
		a.fail(w, err)
		return
	}
	task := ""
	if list, ok := t["tasks"].([]any); ok && len(list) == 1 {
		if first, ok := list[0].(map[string]any); ok {
			task, _ = first["task_id"].(string)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mission_id": mission, "mission_state": m["state"], "waiting": m["waiting"],
		"task_id": task, "repo": repo, "base_commit": base, "spec_artifact_id": specID})
}

// checkGoal trims the goal and refuses what is not one: empty, too long, a
// control character other than a line break or a tab, a repository a goal
// may not name, no idempotency key.
func checkGoal(b goalBody) (goal, repo string, err error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", store.ErrInvalid, fmt.Sprintf(format, args...))
	}
	if b.IdempotencyKey == "" {
		return "", "", invalid("an idempotency key is required")
	}
	goal = strings.TrimSpace(b.Goal)
	switch n := utf8.RuneCountInString(goal); {
	case !utf8.ValidString(goal):
		return "", "", invalid("the goal is not UTF-8")
	case n == 0:
		return "", "", invalid("a goal says what to do")
	case n > maxGoalRunes:
		return "", "", invalid("a goal is at most %d characters; this one has %d", maxGoalRunes, n)
	}
	for _, c := range goal {
		if unicode.IsControl(c) && c != '\n' && c != '\t' {
			return "", "", invalid("the goal has a control character %U", c)
		}
	}
	if _, ok := GoalRepos[b.Repo]; !ok {
		return "", "", invalid("a goal may name %s, not %q", strings.Join(goalRepoNames(), ", "), b.Repo)
	}
	return goal, b.Repo, nil
}

func goalRepoNames() []string {
	names := make([]string, 0, len(GoalRepos))
	for n := range GoalRepos {
		names = append(names, n)
	}
	return names
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

// goalSpec is the task spec agentd runs: the goal, the house rules and the
// checks, as canonical JSON (agentd's ParseSpec).
func goalSpec(title, goal string, repo GoalRepo) ([]byte, error) {
	if len(repo.Verify) == 0 {
		return nil, errors.New("agentrt goals: a repository without checks")
	}
	var cmds strings.Builder
	for _, c := range repo.Verify {
		cmds.WriteString("    " + strings.Join(c, " ") + "\n")
	}
	text := "# " + title + "\n\n" + goal + "\n\n---\n" +
		"This task comes from a goal the Owner typed in the Console. How to hand it in:\n\n" +
		"- Change what the goal needs in this repository and nothing else.\n" +
		"- Before you hand in, run from the repository root:\n\n" + cmds.String() + "\n" +
		"  The platform runs the same commands on your commit; a failing one sends the work back.\n" +
		"- If the goal is unclear or too big for one change, do the part you can and say in your report what is left and why.\n" +
		"- Your report says what you changed, what you ran and how it ended, and what you could not do.\n"
	verify := make([]any, 0, len(repo.Verify))
	for _, c := range repo.Verify {
		args := make([]any, 0, len(c))
		for _, s := range c {
			args = append(args, s)
		}
		verify = append(verify, args)
	}
	return canon.Encode(map[string]any{"text": text, "verify": verify, "timeout_s": goalTimeoutS})
}
