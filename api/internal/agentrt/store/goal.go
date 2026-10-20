package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seatproto"
)

// Goals (W-31 2.2b, the thin chief of staff; the Owner chose D1 A on
// 2026-10-15). A goal typed in the Console goes to the chief of staff's seat
// first: it becomes a mission and one planning task, in one transaction under
// one key. The planning task's spec is the goal, a snapshot of what the
// runtime knows now and the format of a plan. A fresh session in the seat
// reads it, plans read-only and hands the plan in; the controller checks it
// and records it (planresult.go). The seat's memory is these records, not a
// conversation (WORKMODE-owner-chief-of-staff-2026-10-15 §四).
//
// The snapshot is taken once, when the goal is proposed: a retry under the
// same key gets the first answer, whatever changed since (review 213 A1's
// concern, a mission no retry can finish, cannot arise: mission, spec and
// task are written together or not at all).

// Goal is a goal to propose: its first line as the title, its text, the
// repositories it is about (the Owner 10-19, T26: a goal may name several),
// and how to read the commit of a repository's main its tasks start from.
type Goal struct {
	Title, Text string
	Repos       []string
	Main        func(ctx context.Context, repo string) (string, error)
}

// ErrNoMain: a repository's main could not be read for a new goal.
var ErrNoMain = errors.New("store: the repository's main branch cannot be read")

// GoalSuccess is a goal's mission's success criterion until its plan names
// its own.
const GoalSuccess = "the goal's plan is accepted and every task it splits into is accepted"

// PlanOffPlanReason is why a goal's tasks are off the plan tree.
const PlanOffPlanReason = "a goal from the Console, planned by the chief of staff's seat; the runtime has no plan tree yet"

// GoalMaxRepos is how many repositories one goal may name: its planning task
// has the first checked out and the others beside it, and agentd places at
// most four siblings.
const GoalMaxRepos = 5

// CheckGoalRepos is repos as a goal names them, sorted: at least one, at most
// GoalMaxRepos, each a repository the runtime knows (Repos), none twice.
func CheckGoalRepos(repos []string) ([]string, error) {
	if len(repos) == 0 {
		return nil, invalid("a goal may name %s, not none", strings.Join(RepoNames(), ", "))
	}
	if len(repos) > GoalMaxRepos {
		return nil, invalid("a goal names at most %d repositories, not %d", GoalMaxRepos, len(repos))
	}
	out := slices.Clone(repos)
	slices.Sort(out)
	for i, r := range out {
		if _, ok := Repos[r]; !ok {
			return nil, invalid("a goal may name %s, not %q", strings.Join(RepoNames(), ", "), r)
		}
		if i > 0 && out[i-1] == r {
			return nil, invalid("a goal names %s twice", r)
		}
	}
	return out, nil
}

// ProposeGoal records a goal's mission and its planning task. The key's
// request is the goal and its repositories (a goal of one repository is keyed
// as before, by repo); a key that has an answer gets it without main being
// read (review 216 B1). Each repository's main is read once for a new key,
// outside the transaction: the commits are the bases of the first answer, as
// the snapshot is, and the mission keeps them (scope_json.bases) for the
// tasks its plan makes in each repository. The planning task has the first
// repository checked out at its base and the others beside it, at their main.
func ProposeGoal(ctx context.Context, db DB, w Writer, key string, g Goal) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	repos, err := CheckGoalRepos(g.Repos)
	if err != nil {
		return nil, err
	}
	if g.Title == "" || g.Text == "" || g.Main == nil {
		return nil, invalid("a goal has a title, a text and a reader for main")
	}
	bases := map[string]string{}
	mp, err := MissionProposal{Title: g.Title, Goal: g.Text, Repos: repos, Success: []string{GoalSuccess}, bases: bases}.check()
	if err != nil {
		return nil, err
	}
	title := cutRunes("Plan: "+g.Title, maxTaskTitle)
	readMain := func(ctx context.Context) error {
		for _, r := range repos {
			base, err := g.Main(ctx, r)
			if err != nil || base == "" {
				return fmt.Errorf("%w: %s: %v", ErrNoMain, r, err)
			}
			bases[r] = base
		}
		return nil
	}
	request := map[string]any{"goal": g.Text, "repo": repos[0]}
	if len(repos) > 1 {
		request = map[string]any{"goal": g.Text, "repos": repos}
	}
	return idempotentAs(ctx, db, w, "goals", key, request, readMain,
		func(tx pgx.Tx) (map[string]any, error) {
			m, err := proposeMissionTx(ctx, tx, w, mp, "", nil)
			if err != nil {
				return nil, err
			}
			mission, _ := m["mission_id"].(string)
			snap, err := planSnapshot(ctx, tx, mission)
			if err != nil {
				return nil, err
			}
			spec, err := planSpec(g, repos, bases, mission, snap)
			if err != nil {
				return nil, err
			}
			specID, err := PutArtifact(ctx, tx, w.Actor, "task_spec", "application/json", spec)
			if err != nil {
				return nil, err
			}
			plan := []TaskProposal{{Ref: "plan", OffPlanReason: PlanOffPlanReason, Role: "plan", Class: "goal", Title: title,
				SpecArtifact: specID, Effect: "none", Repo: repos[0], BaseCommit: bases[repos[0]]}}
			if err = checkBatch(plan); err != nil {
				return nil, err
			}
			t, err := proposeTasksTx(ctx, tx, w, mission, plan)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"mission_id": mission, "mission_state": m["state"], "task_id": firstTaskID(t),
				"repo": repos[0], "base_commit": bases[repos[0]], "repos": repos, "bases": anyMap(bases), "spec_artifact_id": specID}
			if why, ok := m["waiting"]; ok {
				out["waiting"] = why
			}
			return out, nil
		})
}

func firstTaskID(t map[string]any) string {
	if list, ok := t["tasks"].([]any); ok && len(list) > 0 {
		if first, ok := list[0].(map[string]any); ok {
			id, _ := first["task_id"].(string)
			return id
		}
	}
	return ""
}

// maxTaskTitle is the longest task title a goal or a plan gives, in runes.
const maxTaskTitle = 100

// cutRunes cuts s to at most n runes, ending in "…" when cut.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// Snapshot bounds: what a planner reads is a page, not the database.
const (
	snapMissions  = 20
	snapTasks     = 60
	snapDecisions = 20
	snapGoalRunes = 300
	// a title or a decision's field, and the whole snapshot (review 269 B4)
	snapTitleRunes = 200
	snapFieldRunes = 300
	snapMaxBytes   = 64 << 10
)

// oneLineRunes is s on one line, cut to n runes.
func oneLineRunes(s string, n int) string {
	return cutRunes(strings.Join(strings.Fields(s), " "), n)
}

// planSpec is a planning task's spec: the goal, the snapshot, the principles
// and the format of the plan (ParsePlan reads it back), as canonical JSON with
// no checks: a plan is checked by the controller, not by commands. A goal of
// several repositories has the first checked out and the others beside it as
// siblings, at their main, for the planner to read.
func planSpec(g Goal, repos []string, bases map[string]string, mission, snapshot string) ([]byte, error) {
	first := repos[0]
	where := "repository " + first
	read := "Read this repository as you need to (it is checked out here at " + bases[first] + "); do not edit anything.\n\n"
	scope := "- Every task is in " + first + ", the goal's repository, and runs its checks listed above. " + ownChecks(Repos[first])
	if len(repos) > 1 {
		where = "repositories " + strings.Join(repos, ", ")
		var beside, own []string
		for _, r := range repos[1:] {
			beside = append(beside, fmt.Sprintf("%s is beside it, at ../%s (the head of its main; its tasks start from %s)", r, r, bases[r]))
		}
		for _, r := range repos {
			own = append(own, "  - "+r+": "+ownChecksOf(Repos[r]))
		}
		read = "Read these repositories as you need to: " + first + " is checked out here at " + bases[first] + "; " +
			strings.Join(beside, "; ") + ". Do not edit anything.\n\n"
		scope = "- Every task is in one of the goal's repositories and runs that repository's checks listed above; a change that\n" +
			"  spans repositories is one task in each, ordered with depends_on. Checks of a plan's own, by repository:\n" +
			strings.Join(own, "")
	}
	text := "# Plan: " + g.Title + "\n\nThe Owner's goal, typed in the Console (mission " + mission + ", " + where + "):\n\n" +
		g.Text + "\n\n---\n## Your seat\n\n" +
		"You are the chief of staff's planner. The Owner sets goals, sets rules and decides what cannot be undone; everything else is\n" +
		"yours and the agents', and he audits it afterwards. Turn this goal into a plan the agents can carry out without him:\n" +
		"what counts as done, what is out of scope, the tasks it splits into, the questions only he can answer and the choices you\n" +
		"made. " + read +
		"## What the runtime knows now\n\n" + snapshot + "\n" +
		"## How to plan\n\n" +
		"- Each task is one change an implementer can make and hand in on its own: say what to change and why, where (files,\n" +
		"  functions), and how to tell it is done. Keep tasks small; " + fmt.Sprint(PlanMaxTasks) + " at most. Order them with depends_on.\n" +
		scope +
		checkouts(repos) +
		coreRule(repos) +
		"- Ask the Owner only what is his: a goal he left open, a rule, something that cannot be undone. Give 2 to 4 options and the\n" +
		"  one you go with; the work goes on with it while he has not answered, so plan the tasks for that option. Usually ask nothing.\n" +
		"- Record each choice you made that he may want to audit: what, what you dropped, why, how to reverse it.\n" +
		"- Do not repeat work an open mission above already does.\n\n" +
		CoreText() +
		seatproto.Duties +
		"## What to hand in\n\n" +
		"Your final message is your report: a few lines on the plan, then exactly one fenced block ```json holding:\n\n" +
		"    {\"mission\": {\"success\": [\"...\"], \"out_of_scope\": [\"...\"], \"max_attempts\": 10, \"max_wall_hours\": 24},\n" +
		"     \"tasks\": [{\"ref\": \"t1\", \"title\": \"...\", \"repo\": \"" + first + "\", \"text\": \"...\",\n" +
		"                \"verify\": " + verifyExample(Repos[first]) + ", \"depends_on\": [], \"core\": false}],\n" +
		"     \"questions\": [{\"question\": \"...\", \"body\": \"...\", \"options\": [\"...\", \"...\"], \"recommended\": 1,\n" +
		"                    \"meanwhile\": \"what goes on until he answers\", \"owner_duty\": \"goal\"}],\n" +
		"     \"decisions\": [{\"decision\": \"...\", \"why\": \"...\", \"dropped\": \"...\", \"reverse\": \"...\"}]}\n\n" +
		"success: 1 to " + fmt.Sprint(PlanMaxCriteria) + " criteria. out_of_scope: up to " + fmt.Sprint(PlanMaxCriteria) + ". max_attempts and max_wall_hours\n" +
		"are optional and may only lower this mission's budget. tasks: 1 to " + fmt.Sprint(PlanMaxTasks) + "; ref is short and unique;\n" +
		"verify holds only the checks you add (maybe none); core is true or false (left out: false). questions: up to " +
		fmt.Sprint(PlanMaxQuestions) + "; recommended is the\n" +
		"1-based option; owner_duty is goal, rule or irreversible. decisions: up to " + fmt.Sprint(PlanMaxDecisions) + ". The controller\n" +
		"checks the block; one that does not read blocks this task and the Owner sees why.\n"
	spec := map[string]any{"text": text}
	if len(repos) > 1 {
		sibs := make([]any, 0, len(repos)-1)
		for _, r := range repos[1:] {
			sibs = append(sibs, map[string]any{"repo": r, "ref": "main"})
		}
		spec["siblings"] = sibs
	}
	return canon.Encode(spec)
}

// coreRule tells a planner when a task is core work (core.go, CoreText):
// the label routing reads, and what the plan's check holds it to.
func coreRule(repos []string) string {
	var with []string
	for _, r := range repos {
		if len(Repos[r].Core) > 0 {
			with = append(with, r)
		}
	}
	if len(with) == 0 {
		return "- No task of this goal is core work (its repositories have none; see Core work below): leave core out.\n"
	}
	return "- Mark a task \"core\": true when it is core work (see Core work below) and false otherwise; you judge which by that\n" +
		"  section, nothing else. A core task's text names the files it changes under its repository's core directories. Core is\n" +
		"  refused on a task in a repository with no core work (" + noCore(repos) + "), on one whose text names no such file,\n" +
		"  and as anything but true or false.\n"
}

// noCore lists the repositories of a goal that have no core work.
func noCore(repos []string) string {
	var out []string
	for _, r := range repos {
		if len(Repos[r].Core) == 0 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "none of this goal's"
	}
	return strings.Join(out, ", ")
}

// checkouts tells a planner what each task works in (T26's first run, PROD
// M-9: a planner that read the goal's repositories side by side planned a
// platform task whose done-when ran the infra checks on the infra tree, which
// that task's checkout does not have, and it failed both rounds).
func checkouts(repos []string) string {
	var b strings.Builder
	b.WriteString("- Each task works in a fresh checkout of its own repository at the commit named above, and in nothing else:\n")
	for _, r := range repos {
		fmt.Fprintf(&b, "  - a task in %s has %s.\n", r, CheckoutOf(r))
	}
	if len(repos) > 1 {
		b.WriteString("  The repositories beside you now are for your reading only; no task has them.\n")
	}
	b.WriteString("  A task cannot read, run or change anything outside its checkout, and neither can its checks: a change, a command to\n" +
		"  run, a check or a way to tell it is done that needs another repository is a task in that repository. depends_on orders\n" +
		"  tasks and nothing more: every task starts from its repository's commit named above, never from another task's work,\n" +
		"  so no task sees what another one changed. A task whose text names ../<repository> its checkout does not have is refused.\n")
	return b.String()
}

// ownChecks says which checks of its own a plan may add to a task in repo:
// tests of a Go module, as checkTest takes them, or none where repo has no
// module a plan may test.
func ownChecks(repo Repo) string {
	if len(repo.TestDirs) == 0 {
		return "This repository takes no\n  checks of a plan's own: leave verify empty.\n"
	}
	return "Add a check only when it proves this task:\n" +
		"  a test of the package you change, written exactly as `go -C " + strings.Join(repo.TestDirs, "|") +
		" test [-run <pattern>] ./<package path>` (nothing else is taken).\n"
}

// ownChecksOf is ownChecks for one repository of several.
func ownChecksOf(repo Repo) string {
	if len(repo.TestDirs) == 0 {
		return "none: leave verify empty.\n"
	}
	return "a test of the package you change, only when it proves the task, written exactly as\n" +
		"    `go -C " + strings.Join(repo.TestDirs, "|") + " test [-run <pattern>] ./<package path>` (nothing else is taken).\n"
}

// verifyExample is the verify of the plan format's example task.
func verifyExample(repo Repo) string {
	if len(repo.TestDirs) == 0 {
		return "[]"
	}
	return `[["go", "-C", "` + repo.TestDirs[0] + `", "test", "./internal/..."]]`
}
