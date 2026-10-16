package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
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
// repository it is about, and how to read the commit of that repository's
// main the plan starts from.
type Goal struct {
	Title, Text, Repo string
	Main              func(context.Context) (string, error)
}

// ErrNoMain: the repository's main could not be read for a new goal.
var ErrNoMain = errors.New("store: the repository's main branch cannot be read")

// GoalSuccess is a goal's mission's success criterion until its plan names
// its own.
const GoalSuccess = "the goal's plan is accepted and every task it splits into is accepted"

// PlanOffPlanReason is why a goal's tasks are off the plan tree.
const PlanOffPlanReason = "a goal from the Console, planned by the chief of staff's seat; the runtime has no plan tree yet"

// ProposeGoal records a goal's mission and its planning task. The key's
// request is the goal and its repository; a key that has an answer gets it
// without main being read (review 216 B1). Main is read once for a new key,
// outside the transaction, and is the base of the first answer, as the
// snapshot is.
func ProposeGoal(ctx context.Context, db DB, w Writer, key string, g Goal) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if _, ok := Repos[g.Repo]; !ok {
		return nil, invalid("a goal may name %s, not %q", strings.Join(RepoNames(), ", "), g.Repo)
	}
	if g.Title == "" || g.Text == "" || g.Main == nil {
		return nil, invalid("a goal has a title, a text and a reader for main")
	}
	mp, err := MissionProposal{Title: g.Title, Goal: g.Text, Repos: []string{g.Repo}, Success: []string{GoalSuccess}}.check()
	if err != nil {
		return nil, err
	}
	title := cutRunes("Plan: "+g.Title, maxTaskTitle)
	var base string
	readMain := func(ctx context.Context) error {
		var err error
		if base, err = g.Main(ctx); err != nil || base == "" {
			return fmt.Errorf("%w: %s: %v", ErrNoMain, g.Repo, err)
		}
		return nil
	}
	return idempotent(ctx, db, w.Actor, "goals", key, map[string]any{"goal": g.Text, "repo": g.Repo}, readMain,
		func(tx pgx.Tx) (map[string]any, error) {
			m, err := proposeMissionTx(ctx, tx, w, mp)
			if err != nil {
				return nil, err
			}
			mission, _ := m["mission_id"].(string)
			snap, err := planSnapshot(ctx, tx, mission)
			if err != nil {
				return nil, err
			}
			spec, err := planSpec(g, base, mission, snap)
			if err != nil {
				return nil, err
			}
			specID, err := PutArtifact(ctx, tx, w.Actor, "task_spec", "application/json", spec)
			if err != nil {
				return nil, err
			}
			plan := []TaskProposal{{Ref: "plan", OffPlanReason: PlanOffPlanReason, Role: "plan", Class: "goal", Title: title,
				SpecArtifact: specID, Effect: "none", Repo: g.Repo, BaseCommit: base}}
			if err = checkBatch(plan); err != nil {
				return nil, err
			}
			t, err := proposeTasksTx(ctx, tx, w, mission, plan)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"mission_id": mission, "mission_state": m["state"], "task_id": firstTaskID(t),
				"repo": g.Repo, "base_commit": base, "spec_artifact_id": specID}
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
)

// planSnapshot is what the runtime knows now, for a planner (the five records
// of WORKMODE §四 the runtime holds): the missions open besides this one and
// their unfinished tasks, the latest decisions recorded, the standing
// envelope in force and this mission's budget, the repositories a plan may
// name and their checks. The fifth, the design and its principles, is in the
// spec's own text.
func planSnapshot(ctx context.Context, tx pgx.Tx, mission string) (string, error) {
	var b strings.Builder
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "Taken %s.\n\n### Missions open besides this one\n\n", now.UTC().Format(time.RFC3339))
	rows, err := tx.Query(ctx, `SELECT mission_id, state, title, goal FROM agentrt.mission
	  WHERE state IN ('proposed','awaiting_owner','active','paused') AND mission_id <> $1
	  ORDER BY created_at DESC, mission_id LIMIT $2`, mission, snapMissions)
	if err != nil {
		return "", err
	}
	var open []string
	for rows.Next() {
		var id, state, title, goal string
		if err = rows.Scan(&id, &state, &title, &goal); err != nil {
			rows.Close()
			return "", err
		}
		open = append(open, id)
		fmt.Fprintf(&b, "- %s (%s): %s — %s\n", id, state, title, oneLineRunes(goal, snapGoalRunes))
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(open) == 0 {
		b.WriteString("None.\n")
	}
	b.WriteString("\n### Their unfinished tasks\n\n")
	rows, err = tx.Query(ctx, `SELECT mission_id, task_id, role, state, title FROM agentrt.task
	  WHERE mission_id = ANY($1) AND state NOT IN ('accepted','rejected','cancelled')
	  ORDER BY mission_id, created_at, task_id LIMIT $2`, open, snapTasks)
	if err != nil {
		return "", err
	}
	n := 0
	for rows.Next() {
		var m, id, role, state, title string
		if err = rows.Scan(&m, &id, &role, &state, &title); err != nil {
			rows.Close()
			return "", err
		}
		n++
		fmt.Fprintf(&b, "- %s %s (%s, %s): %s\n", m, id, role, state, title)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		b.WriteString("None.\n")
	}
	b.WriteString("\n### Latest decisions recorded (newest first)\n\n")
	rows, err = tx.Query(ctx, `SELECT COALESCE(mission_id, ''), at, body_json FROM agentrt.event
	  WHERE event_type = 'decision_recorded' ORDER BY event_id DESC LIMIT $1`, snapDecisions)
	if err != nil {
		return "", err
	}
	n = 0
	for rows.Next() {
		var m string
		var at time.Time
		var body []byte
		if err = rows.Scan(&m, &at, &body); err != nil {
			rows.Close()
			return "", err
		}
		var d map[string]string
		_ = json.Unmarshal(body, &d)
		n++
		fmt.Fprintf(&b, "- %s %s: %s — why: %s", at.UTC().Format("2006-01-02"), m, d["decision"], d["why"])
		if d["dropped"] != "" {
			fmt.Fprintf(&b, " — dropped: %s", d["dropped"])
		}
		if d["reverse"] != "" {
			fmt.Fprintf(&b, " — to reverse: %s", d["reverse"])
		}
		b.WriteString("\n")
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		b.WriteString("None.\n")
	}
	b.WriteString("\n### Standing envelope and this mission's budget\n\n")
	env, err := inForce(ctx, tx, rules.KindEnvelope+":dev")
	if err != nil {
		return "", err
	}
	if env == nil || env.Envelope == nil {
		b.WriteString("No standing envelope is in force: every mission waits for the Owner.\n")
	} else {
		fmt.Fprintf(&b, "%s v%d: up to %s, at most %d attempts and %d hours a mission, executors %s, hosts %s.\n",
			env.ID, env.Version, env.Envelope.MaxAction, env.Envelope.Budget.MaxAttempts, env.Envelope.Budget.MaxWallHours,
			strings.Join(env.Envelope.Executors, ", "), strings.Join(env.Envelope.Hosts, ", "))
	}
	var budget []byte
	if err = tx.QueryRow(ctx, `SELECT budget_json FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&budget); err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "This mission (%s): budget %s.\n\n### Repositories a plan may name, and the checks every task runs\n\n", mission, budget)
	for _, name := range RepoNames() {
		var cmds []string
		for _, c := range Repos[name].Verify {
			cmds = append(cmds, "`"+strings.Join(c, " ")+"`")
		}
		fmt.Fprintf(&b, "- %s: %s\n", name, strings.Join(cmds, ", "))
	}
	return b.String(), nil
}

// oneLineRunes is s on one line, cut to n runes.
func oneLineRunes(s string, n int) string {
	return cutRunes(strings.Join(strings.Fields(s), " "), n)
}

// planSpec is a planning task's spec: the goal, the snapshot, the principles
// and the format of the plan (ParsePlan reads it back), as canonical JSON with
// no checks: a plan is checked by the controller, not by commands.
func planSpec(g Goal, base, mission, snapshot string) ([]byte, error) {
	text := "# Plan: " + g.Title + "\n\nThe Owner's goal, typed in the Console (mission " + mission + ", repository " + g.Repo + "):\n\n" +
		g.Text + "\n\n---\n## Your seat\n\n" +
		"You are the chief of staff's planner. The Owner sets goals, sets rules and decides what cannot be undone; everything else is\n" +
		"yours and the agents', and he audits it afterwards. Turn this goal into a plan the agents can carry out without him:\n" +
		"what counts as done, what is out of scope, the tasks it splits into, the questions only he can answer and the choices you\n" +
		"made. Read this repository as you need to (it is checked out here at " + base + "); do not edit anything.\n\n" +
		"## What the runtime knows now\n\n" + snapshot + "\n" +
		"## How to plan\n\n" +
		"- Each task is one change an implementer can make and hand in on its own: say what to change and why, where (files,\n" +
		"  functions), and how to tell it is done. Keep tasks small; " + fmt.Sprint(PlanMaxTasks) + " at most. Order them with depends_on.\n" +
		"- Every task runs the repository's checks listed above. Add a check only when it proves this task: a test of the package\n" +
		"  you change, written exactly as `go -C api test [-run <pattern>] ./<package path>` (nothing else is taken).\n" +
		"- Ask the Owner only what is his: a goal he left open, a rule, something that cannot be undone. Give 2 to 4 options and the\n" +
		"  one you go with; the work goes on with it while he has not answered, so plan the tasks for that option. Usually ask nothing.\n" +
		"- Record each choice you made that he may want to audit: what, what you dropped, why, how to reverse it.\n" +
		"- Do not repeat work an open mission above already does.\n\n" +
		"## What to hand in\n\n" +
		"Your final message is your report: a few lines on the plan, then exactly one fenced block ```json holding:\n\n" +
		"    {\"mission\": {\"success\": [\"...\"], \"out_of_scope\": [\"...\"], \"max_attempts\": 10, \"max_wall_hours\": 24},\n" +
		"     \"tasks\": [{\"ref\": \"t1\", \"title\": \"...\", \"repo\": \"" + g.Repo + "\", \"text\": \"...\",\n" +
		"                \"verify\": [[\"go\", \"-C\", \"api\", \"test\", \"./internal/...\"]], \"depends_on\": []}],\n" +
		"     \"questions\": [{\"question\": \"...\", \"body\": \"...\", \"options\": [\"...\", \"...\"], \"recommended\": 1,\n" +
		"                    \"meanwhile\": \"what goes on until he answers\", \"owner_duty\": \"goal\"}],\n" +
		"     \"decisions\": [{\"decision\": \"...\", \"why\": \"...\", \"dropped\": \"...\", \"reverse\": \"...\"}]}\n\n" +
		"success: 1 to " + fmt.Sprint(PlanMaxCriteria) + " criteria. out_of_scope: up to " + fmt.Sprint(PlanMaxCriteria) + ". max_attempts and max_wall_hours\n" +
		"are optional and may only lower this mission's budget. tasks: 1 to " + fmt.Sprint(PlanMaxTasks) + "; ref is short and unique;\n" +
		"verify holds only the checks you add (maybe none). questions: up to " + fmt.Sprint(PlanMaxQuestions) + "; recommended is the\n" +
		"1-based option; owner_duty is goal, rule or irreversible. decisions: up to " + fmt.Sprint(PlanMaxDecisions) + ". The controller\n" +
		"checks the block; one that does not read blocks this task and the Owner sees why.\n"
	return canon.Encode(map[string]any{"text": text})
}
