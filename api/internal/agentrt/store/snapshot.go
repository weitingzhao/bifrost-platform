package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// What the runtime knows now, for the chief of staff (the five records of
// WORKMODE §四 the runtime holds): the missions open besides the one asking
// and their unfinished tasks, the latest decisions recorded, the standing
// envelope in force and the mission's budget, the repositories the runtime
// knows and their checks (a goal's plan names only the goal's: planSpec).
// The fifth, the design and its principles, is in the spec's own text.
//
// planSnapshot reads the records; RenderSnapshot writes them, and nothing
// else: the chief exam renders its fixed records with the same function, so
// what the seat reads in an exam is what it reads at work (the exam's cut
// §4.2).

// Snapshot is the records a snapshot is written from.
type Snapshot struct {
	Taken     time.Time
	Missions  []SnapshotMission  // open besides this one, newest first
	Tasks     []SnapshotTask     // theirs, not finished
	Decisions []SnapshotDecision // newest first
	// Envelope is the standing envelope in force, written as a line; ""
	// when none is.
	Envelope string
	Mission  string // the mission the snapshot is for
	Budget   string // its budget, as the database writes budget_json
}

// SnapshotMission is an open mission.
type SnapshotMission struct{ ID, State, Title, Goal string }

// SnapshotTask is an open mission's unfinished task.
type SnapshotTask struct{ Mission, ID, Role, State, Title string }

// SnapshotDecision is a decision recorded.
type SnapshotDecision struct {
	At                                       time.Time
	Mission, Decision, Why, Dropped, Reverse string
}

// EnvelopeLine is how a snapshot writes a standing envelope in force.
func EnvelopeLine(id string, version int, e *rules.Envelope) string {
	return fmt.Sprintf("%s v%d: up to %s, at most %d attempts and %d hours a mission, executors %s, hosts %s.",
		id, version, e.MaxAction, e.Budget.MaxAttempts, e.Budget.MaxWallHours,
		strings.Join(e.Executors, ", "), strings.Join(e.Hosts, ", "))
}

// RenderSnapshot writes s as the seat reads it: each field cut, the whole
// bounded by cutting the records and keeping the envelope, the budget and the
// checks whole (reviews 269 B4, 270 B4).
func RenderSnapshot(s Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Taken %s.\n\n### Missions open besides this one\n\n", s.Taken.UTC().Format(time.RFC3339))
	for _, m := range s.Missions {
		fmt.Fprintf(&b, "- %s (%s): %s — %s\n", m.ID, m.State, oneLineRunes(m.Title, snapTitleRunes), oneLineRunes(m.Goal, snapGoalRunes))
	}
	if len(s.Missions) == 0 {
		b.WriteString("None.\n")
	}
	b.WriteString("\n### Their unfinished tasks\n\n")
	for _, t := range s.Tasks {
		fmt.Fprintf(&b, "- %s %s (%s, %s): %s\n", t.Mission, t.ID, t.Role, t.State, oneLineRunes(t.Title, snapTitleRunes))
	}
	if len(s.Tasks) == 0 {
		b.WriteString("None.\n")
	}
	b.WriteString("\n### Latest decisions recorded (newest first)\n\n")
	for _, d := range s.Decisions {
		fmt.Fprintf(&b, "- %s %s: %s — why: %s", d.At.UTC().Format("2006-01-02"), d.Mission, oneLineRunes(d.Decision, snapFieldRunes),
			oneLineRunes(d.Why, snapFieldRunes))
		if d.Dropped != "" {
			fmt.Fprintf(&b, " — dropped: %s", oneLineRunes(d.Dropped, snapFieldRunes))
		}
		if d.Reverse != "" {
			fmt.Fprintf(&b, " — to reverse: %s", oneLineRunes(d.Reverse, snapFieldRunes))
		}
		b.WriteString("\n")
	}
	if len(s.Decisions) == 0 {
		b.WriteString("None.\n")
	}
	records := b.Len() // what comes after is fixed and kept whole (review 270 B4)
	b.WriteString("\n### Standing envelope and this mission's budget\n\n")
	if s.Envelope == "" {
		b.WriteString("No standing envelope is in force: every mission waits for the Owner.\n")
	} else {
		b.WriteString(s.Envelope + "\n")
	}
	fmt.Fprintf(&b, "This mission (%s): budget %s.\n\n### Repositories the runtime knows, and the checks every task in each runs\n\n", s.Mission, s.Budget)
	for _, name := range RepoNames() {
		var cmds []string
		for _, c := range Repos[name].Verify {
			cmds = append(cmds, "`"+shellLine(c)+"`")
		}
		fmt.Fprintf(&b, "- %s: %s\n", name, strings.Join(cmds, ", "))
	}
	out := b.String()
	if len(out) > snapMaxBytes {
		const mark = "\n(The records above were longer; they are cut here.)\n"
		head, fixed := out[:records], out[records:]
		n := max(snapMaxBytes-len(fixed)-len(mark), 0)
		cut := []byte(head[:min(n, len(head))])
		for !utf8.Valid(cut) {
			cut = cut[:len(cut)-1]
		}
		out = string(cut) + mark + fixed
	}
	return out
}

// planSnapshot reads the snapshot's records for mission and writes them.
func planSnapshot(ctx context.Context, tx pgx.Tx, mission string) (string, error) {
	s := Snapshot{Mission: mission}
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&s.Taken); err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, `SELECT mission_id, state, title, goal FROM agentrt.mission
	  WHERE state IN ('proposed','awaiting_owner','active','paused') AND mission_id <> $1
	  ORDER BY created_at DESC, mission_id LIMIT $2`, mission, snapMissions)
	if err != nil {
		return "", err
	}
	var open []string
	for rows.Next() {
		var m SnapshotMission
		if err = rows.Scan(&m.ID, &m.State, &m.Title, &m.Goal); err != nil {
			rows.Close()
			return "", err
		}
		open = append(open, m.ID)
		s.Missions = append(s.Missions, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", err
	}
	rows, err = tx.Query(ctx, `SELECT mission_id, task_id, role, state, title FROM agentrt.task
	  WHERE mission_id = ANY($1) AND state NOT IN ('accepted','rejected','cancelled')
	  ORDER BY mission_id, created_at, task_id LIMIT $2`, open, snapTasks)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var t SnapshotTask
		if err = rows.Scan(&t.Mission, &t.ID, &t.Role, &t.State, &t.Title); err != nil {
			rows.Close()
			return "", err
		}
		s.Tasks = append(s.Tasks, t)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", err
	}
	rows, err = tx.Query(ctx, `SELECT COALESCE(mission_id, ''), at, body_json FROM agentrt.event
	  WHERE event_type = 'decision_recorded' ORDER BY event_id DESC LIMIT $1`, snapDecisions)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var d SnapshotDecision
		var body []byte
		if err = rows.Scan(&d.Mission, &d.At, &body); err != nil {
			rows.Close()
			return "", err
		}
		var f map[string]string
		_ = json.Unmarshal(body, &f)
		d.Decision, d.Why, d.Dropped, d.Reverse = f["decision"], f["why"], f["dropped"], f["reverse"]
		s.Decisions = append(s.Decisions, d)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", err
	}
	env, err := inForce(ctx, tx, rules.KindEnvelope+":dev")
	if err != nil {
		return "", err
	}
	if env != nil && env.Envelope != nil {
		s.Envelope = EnvelopeLine(env.ID, env.Version, env.Envelope)
	}
	var budget []byte
	if err = tx.QueryRow(ctx, `SELECT budget_json FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&budget); err != nil {
		return "", err
	}
	s.Budget = string(budget)
	return RenderSnapshot(s), nil
}
