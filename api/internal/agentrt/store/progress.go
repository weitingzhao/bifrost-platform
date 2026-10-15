package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// The progress projection (GET /progress; the interface is fixed in
// SPEC-agentrt-progress-2026-10-14.md under agent-config/work/multi-agent):
// computed on every read from the runtime's own rows, in one read-only
// snapshot, never stored. What cannot be computed is left out, never filled
// with a value that looks normal.

// Task statuses, in the order a mission shows the one most worth seeing.
const (
	StatusHostLost     = "host_lost"
	StatusMaybeStuck   = "maybe_stuck"
	StatusWaitingOwner = "waiting_owner"
	StatusBlocked      = "blocked"
	StatusSlow         = "slow"
	StatusWorking      = "working"
	StatusWaitingRole  = "waiting_role"
	StatusQueued       = "queued"
)

var statusRank = []string{StatusHostLost, StatusMaybeStuck, StatusWaitingOwner, StatusBlocked, StatusSlow, StatusWorking,
	StatusWaitingRole, StatusQueued}

// HostLostAfter is how long since its last poll a host counts as lost (T24:
// shown lost within 3 to 4 minutes).
const HostLostAfter = 3 * time.Minute

// SlowFactor: a working attempt is slow past this many times the usual.
const SlowFactor = 1.5

// usualMinSamples and usualWindow bound the history a usual time is read
// from: fewer samples than this give none.
const (
	usualMinSamples = 3
	usualWindow     = 30 * 24 * time.Hour
)

// ProgressAttempt is a live attempt as the projection shows it.
type ProgressAttempt struct {
	AttemptID      string `json:"attempt_id"`
	AttemptNo      int    `json:"attempt_no"`
	TaskID         string `json:"task_id,omitempty"`
	MissionID      string `json:"mission_id,omitempty"`
	HostID         string `json:"host_id,omitempty"`
	Executor       string `json:"executor"`
	Model          string `json:"model,omitempty"`
	State          string `json:"state"`
	StartedAt      string `json:"started_at,omitempty"`
	ElapsedS       *int64 `json:"elapsed_s,omitempty"`
	HeartbeatAgeS  int64  `json:"heartbeat_age_s"`
	CheckpointSeq  *int64 `json:"checkpoint_seq,omitempty"`
	Status         string `json:"status,omitempty"`
	hostPollAgeS   *int64
	elapsed        *time.Duration
	role, class    string
	taskStateValue string
}

// ProgressTask is an unfinished task of a mission.
type ProgressTask struct {
	TaskID     string           `json:"task_id"`
	Title      string           `json:"title"`
	Role       string           `json:"role"`
	PlanNodeID string           `json:"plan_node_id,omitempty"`
	RoundNo    int              `json:"round_no"`
	State      string           `json:"state"`
	Status     string           `json:"status"`
	Attempt    *ProgressAttempt `json:"attempt,omitempty"`
	UsualS     *int64           `json:"usual_s,omitempty"`
	class      string
}

// TaskStatus is the status of a task (SPEC §3.1): its live attempt, if any,
// and its mission's state decide it.
func TaskStatus(taskState, missionState string, a *ProgressAttempt, usual *time.Duration) string {
	if a != nil {
		switch {
		case a.hostPollAgeS == nil || time.Duration(*a.hostPollAgeS)*time.Second > HostLostAfter:
			return StatusHostLost
		case a.State == "stale":
			return StatusMaybeStuck
		}
	}
	switch {
	case taskState == "awaiting_owner" || missionState == "proposed" || missionState == "awaiting_owner":
		return StatusWaitingOwner
	case taskState == "blocked" || taskState == "paused":
		return StatusBlocked
	}
	if a != nil {
		if usual != nil && a.elapsed != nil && float64(*a.elapsed) > SlowFactor*float64(*usual) {
			return StatusSlow
		}
		return StatusWorking
	}
	switch taskState {
	case "submitted", "verifying", "rework":
		return StatusWaitingRole
	}
	return StatusQueued
}

// worst is the status most worth seeing among statuses.
func worst(statuses []string) string {
	best := len(statusRank)
	for _, s := range statuses {
		if i := slices.Index(statusRank, s); i >= 0 && i < best {
			best = i
		}
	}
	if best == len(statusRank) {
		return ""
	}
	return statusRank[best]
}

// Progress computes the projection.
func Progress(ctx context.Context, db DB) (map[string]any, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	usual, err := usualTimes(ctx, tx)
	if err != nil {
		return nil, err
	}
	live, err := liveAttempts(ctx, tx, usual)
	if err != nil {
		return nil, err
	}
	work, err := missionsInWork(ctx, tx, live, usual)
	if err != nil {
		return nil, err
	}
	needs, err := needsYou(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	planDoc, err := planCounts(ctx, tx)
	if err != nil {
		return nil, err
	}
	rest := map[string]string{
		"hosts": `SELECT COALESCE(jsonb_agg(to_jsonb(h) || jsonb_build_object(
		    'poll_age_s', floor(extract(epoch FROM clock_timestamp() - h.last_poll_at)),
		    'online', COALESCE(h.last_poll_at > clock_timestamp() - make_interval(secs => ` + fmt.Sprint(HostLostAfter.Seconds()) + `), false)) ORDER BY h.host_id), '[]'::jsonb)
		  FROM agentrt.host h`,
		"controls": `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.set_at DESC), '[]'::jsonb) FROM agentrt.control c WHERE c.cleared_at IS NULL`,
		"recent": `SELECT COALESCE(jsonb_agg(r ORDER BY r.ended_at DESC), '[]'::jsonb) FROM (
		    SELECT t.task_id, t.mission_id, t.title, t.role, t.state, t.updated_at AS ended_at, a.conclusion, a.report_artifact_id
		    FROM agentrt.task t
		    LEFT JOIN LATERAL (SELECT conclusion, report_artifact_id FROM agentrt.attempt WHERE task_id = t.task_id
		      ORDER BY attempt_no DESC LIMIT 1) a ON true
		    WHERE t.state IN ('accepted','rejected','cancelled') ORDER BY t.updated_at DESC LIMIT 20) r`,
	}
	out := map[string]any{"generated_at": now.UTC().Format(time.RFC3339Nano), "needs_you": needs, "work": work, "plan": planDoc}
	liveDoc := make([]any, 0, len(live))
	for _, a := range sortedLive(live) {
		liveDoc = append(liveDoc, a)
	}
	out["live"] = liveDoc
	for key, q := range rest {
		var doc []byte
		if err = tx.QueryRow(ctx, q).Scan(&doc); err != nil {
			return nil, fmt.Errorf("progress %s: %w", key, err)
		}
		out[key] = rawJSON(doc)
	}
	return out, nil
}

// rawJSON is a document the database built, passed through as it is.
type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }

// usualTimes reads, per role and task class, the median time from start to
// computed of the attempts that ended done on tasks accepted in the window;
// fewer than usualMinSamples give none (SPEC §3.2).
func usualTimes(ctx context.Context, tx pgx.Tx) (map[string]time.Duration, error) {
	rows, err := tx.Query(ctx, `SELECT t.role || '|' || t.task_class,
	    percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM a.computed_at - a.started_at)), count(*)
	  FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
	  WHERE t.state = 'accepted' AND a.conclusion = 'done' AND a.started_at IS NOT NULL AND a.computed_at IS NOT NULL
	    AND t.updated_at > clock_timestamp() - make_interval(secs => $1)
	  GROUP BY 1`, usualWindow.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Duration{}
	for rows.Next() {
		var key string
		var median float64
		var n int
		if err = rows.Scan(&key, &median, &n); err != nil {
			return nil, err
		}
		if n >= usualMinSamples {
			out[key] = time.Duration(median * float64(time.Second))
		}
	}
	return out, rows.Err()
}

func liveAttempts(ctx context.Context, tx pgx.Tx, usual map[string]time.Duration) (map[string]*ProgressAttempt, error) {
	rows, err := tx.Query(ctx, `SELECT a.attempt_id, a.attempt_no, a.task_id, t.mission_id, COALESCE(a.host_id, ''), a.executor_actor_id,
	    COALESCE(a.model, ''), a.state, a.started_at,
	    floor(extract(epoch FROM clock_timestamp() - a.started_at))::bigint,
	    floor(extract(epoch FROM clock_timestamp() - a.heartbeat_at))::bigint,
	    floor(extract(epoch FROM clock_timestamp() - h.last_poll_at))::bigint,
	    (SELECT max((e.body_json->>'checkpoint_seq')::bigint) FROM agentrt.event e
	      WHERE e.attempt_id = a.attempt_id AND e.event_type = 'attempt_checkpointed'),
	    t.role, t.task_class, t.state
	  FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
	  LEFT JOIN agentrt.host h ON h.host_id = a.host_id
	  WHERE a.state IN ('leased','running','stale')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ProgressAttempt{}
	for rows.Next() {
		a := &ProgressAttempt{}
		var started *time.Time
		if err = rows.Scan(&a.AttemptID, &a.AttemptNo, &a.TaskID, &a.MissionID, &a.HostID, &a.Executor, &a.Model, &a.State, &started,
			&a.ElapsedS, &a.HeartbeatAgeS, &a.hostPollAgeS, &a.CheckpointSeq, &a.role, &a.class, &a.taskStateValue); err != nil {
			return nil, err
		}
		if started != nil {
			a.StartedAt = started.UTC().Format(time.RFC3339Nano)
		}
		if a.ElapsedS != nil {
			d := time.Duration(*a.ElapsedS) * time.Second
			a.elapsed = &d
		}
		out[a.TaskID] = a
	}
	return out, rows.Err()
}

func sortedLive(live map[string]*ProgressAttempt) []*ProgressAttempt {
	list := make([]*ProgressAttempt, 0, len(live))
	for _, a := range live {
		list = append(list, a)
	}
	slices.SortFunc(list, func(x, y *ProgressAttempt) int {
		if c := strings.Compare(x.StartedAt, y.StartedAt); c != 0 {
			return c
		}
		return strings.Compare(x.AttemptID, y.AttemptID)
	})
	return list
}

func usualOf(usual map[string]time.Duration, role, class string) *time.Duration {
	if d, ok := usual[role+"|"+class]; ok {
		return &d
	}
	return nil
}

// sliceRoles are the roles whose tasks are a mission's slices; verification
// and review are steps of a slice (SPEC §2.2).
var sliceRoles = []string{"plan", "implement", "brief", "other"}

func missionsInWork(ctx context.Context, tx pgx.Tx, live map[string]*ProgressAttempt, usual map[string]time.Duration) ([]any, error) {
	type mission struct {
		id, title, goal, state, deadline, work string
		done, total                            int
		tasks                                  []*ProgressTask
	}
	rows, err := tx.Query(ctx, `SELECT m.mission_id, m.title, m.goal, m.state, m.deadline_at, COALESCE(m.work_id, ''),
	    count(t.task_id) FILTER (WHERE t.role = ANY($1) AND t.state = 'accepted'),
	    count(t.task_id) FILTER (WHERE t.role = ANY($1) AND t.state NOT IN ('rejected','cancelled'))
	  FROM agentrt.mission m LEFT JOIN agentrt.task t ON t.mission_id = m.mission_id
	  WHERE m.state IN ('proposed','awaiting_owner','active','paused')
	  GROUP BY m.mission_id ORDER BY substr(m.mission_id, 3)::int`, sliceRoles)
	if err != nil {
		return nil, err
	}
	var missions []*mission
	byID := map[string]*mission{}
	for rows.Next() {
		m := &mission{}
		var deadline time.Time
		if err = rows.Scan(&m.id, &m.title, &m.goal, &m.state, &deadline, &m.work, &m.done, &m.total); err != nil {
			rows.Close()
			return nil, err
		}
		m.deadline = deadline.UTC().Format(time.RFC3339)
		missions = append(missions, m)
		byID[m.id] = m
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	trows, err := tx.Query(ctx, `SELECT t.task_id, t.mission_id, t.title, t.role, COALESCE(t.plan_node_id, ''), t.round_no, t.state, t.task_class
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  WHERE m.state IN ('proposed','awaiting_owner','active','paused') AND t.state NOT IN ('accepted','rejected','cancelled')
	  ORDER BY t.created_at, t.task_id`)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		t := &ProgressTask{}
		var missionID string
		if err = trows.Scan(&t.TaskID, &missionID, &t.Title, &t.Role, &t.PlanNodeID, &t.RoundNo, &t.State, &t.class); err != nil {
			return nil, err
		}
		m := byID[missionID]
		if m == nil {
			continue
		}
		t.Attempt = live[t.TaskID]
		u := usualOf(usual, t.Role, t.class)
		if u != nil {
			s := int64(*u / time.Second)
			t.UsualS = &s
		}
		t.Status = TaskStatus(t.State, m.state, t.Attempt, u)
		if t.Attempt != nil {
			t.Attempt.Status = t.Status
		}
		m.tasks = append(m.tasks, t)
	}
	if err = trows.Err(); err != nil {
		return nil, err
	}
	// A live attempt of a task outside the missions in work still gets its
	// status, for the live list.
	for _, a := range live {
		if a.Status == "" {
			a.Status = TaskStatus(a.taskStateValue, "", a, usualOf(usual, a.role, a.class))
		}
	}
	out := make([]any, 0, len(missions))
	for _, m := range missions {
		statuses := make([]string, 0, len(m.tasks))
		tasks := make([]any, 0, len(m.tasks))
		for _, t := range m.tasks {
			statuses = append(statuses, t.Status)
			tasks = append(tasks, t)
		}
		status := worst(statuses)
		if status == "" {
			status = "idle"
			if m.state == "proposed" || m.state == "awaiting_owner" {
				status = StatusWaitingOwner
			}
		}
		out = append(out, map[string]any{"mission_id": m.id, "title": m.title, "goal": m.goal, "state": m.state,
			"deadline_at": m.deadline, "work_id": m.work, "slices": map[string]int{"done": m.done, "total": m.total},
			"tasks": tasks, "status": status})
	}
	return out, nil
}

// needsYou lists what waits for the Owner (SPEC §2.1): plan changes on a
// decision card, missions waiting for a standing envelope, blocked tasks,
// owner steps. count is the items he can act on.
func needsYou(ctx context.Context, tx pgx.Tx, now time.Time) (map[string]any, error) {
	rows, err := tx.Query(ctx, `
	  SELECT 'plan_change', p.event_id::text, 'Plan change to ' || substr(p.event_stream_id, 6) || ': ' || (p.body_json->>'reason'),
	    p.at, p.body_json->>'question_id', '', ''
	  FROM agentrt.event p
	  WHERE p.event_type = 'plan_change_proposed' AND p.body_json ? 'question_id'
	    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided'
	      AND d.event_stream_id = p.event_stream_id AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
	  UNION ALL
	  SELECT 'mission', m.mission_id, m.title,
	    COALESCE((SELECT max(e.at) FROM agentrt.event e WHERE e.mission_id = m.mission_id
	      AND e.event_type IN ('mission_proposed','mission_state_changed')), m.created_at), '', m.mission_id, ''
	  FROM agentrt.mission m WHERE m.state IN ('proposed','awaiting_owner')
	  UNION ALL
	  SELECT 'task_blocked', t.task_id, t.title, t.updated_at, '', t.mission_id, t.state
	  FROM agentrt.task t WHERE t.state IN ('blocked','awaiting_owner')
	  UNION ALL
	  SELECT 'owner_step', n.plan_node_id, n.title, COALESCE(e.at, n.created_at), '', COALESCE(n.mission_id, ''), ''
	  FROM agentrt.plan_node n LEFT JOIN agentrt.event e ON e.event_id = n.state_event_id
	  WHERE n.node_kind = 'step' AND (n.step_kind = 'owner' OR n.owner_step) AND n.state = 'active'
	  ORDER BY 4`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []any{}
	count := 0
	for rows.Next() {
		var kind, ref, title, question, mission, state string
		var since time.Time
		if err = rows.Scan(&kind, &ref, &title, &since, &question, &mission, &state); err != nil {
			return nil, err
		}
		item := map[string]any{"kind": kind, "ref": ref, "title": title, "since": since.UTC().Format(time.RFC3339Nano),
			"waiting_min": int64(now.Sub(since) / time.Minute)}
		var action map[string]any
		switch kind {
		case "plan_change":
			action = map[string]any{"where": "console", "question_id": question}
		case "mission":
			action = map[string]any{"where": "none", "why": "the mission waits for a signed standing-envelope rule (the rules format waits for the Owner)"}
		case "task_blocked":
			action = map[string]any{"where": "none", "why": "the task is " + state + "; the coordinator decides first, and a decision card appears here when it is the Owner's"}
		case "owner_step":
			action = map[string]any{"where": "console", "plan_node_id": ref}
		}
		item["action"] = action
		if mission != "" {
			item["blocks"] = []string{mission}
		} else {
			item["blocks"] = []string{}
		}
		if action["where"] != "none" {
			count++
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"count": count, "items": items}, nil
}

// planCounts is each root with its count and its items' counts (design v5
// §5.1.2 「计数」), and the steps in work.
func planCounts(ctx context.Context, tx pgx.Tx) ([]any, error) {
	rows, err := tx.Query(ctx, `SELECT r.plan_node_id, r.title, i.plan_node_id, i.label, i.title, i.ordinal,
	    s.plan_node_id, s.state, COALESCE(e.body_json->>'cause', '')
	  FROM agentrt.plan_node r
	  LEFT JOIN agentrt.plan_node i ON i.parent_plan_node_id = r.plan_node_id AND i.node_kind IN ('item','arch_row')
	  LEFT JOIN agentrt.plan_node s ON s.parent_plan_node_id = i.plan_node_id AND s.node_kind = 'step'
	  LEFT JOIN agentrt.event e ON e.event_id = s.state_event_id
	  WHERE r.parent_plan_node_id IS NULL
	  ORDER BY r.node_kind, r.ordinal, r.plan_node_id, i.ordinal, s.ordinal`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type item struct {
		id, label, title string
		steps            []plan.Step
		active           []string
	}
	type root struct {
		id, title string
		items     []*item
	}
	var roots []*root
	byRoot := map[string]*root{}
	byItem := map[string]*item{}
	for rows.Next() {
		var rid, rtitle string
		var iid, ilabel, ititle, sid, sstate *string
		var iord *int
		var cause string
		if err = rows.Scan(&rid, &rtitle, &iid, &ilabel, &ititle, &iord, &sid, &sstate, &cause); err != nil {
			return nil, err
		}
		r := byRoot[rid]
		if r == nil {
			r = &root{id: rid, title: rtitle}
			byRoot[rid] = r
			roots = append(roots, r)
		}
		if iid == nil {
			continue
		}
		it := byItem[*iid]
		if it == nil {
			it = &item{id: *iid, label: *ilabel, title: *ititle}
			byItem[*iid] = it
			r.items = append(r.items, it)
		}
		if sid == nil || sstate == nil {
			continue
		}
		it.steps = append(it.steps, plan.Step{ID: *sid, State: *sstate, Manual: *sstate == plan.StateDone && cause == "done_rule_manual"})
		if *sstate == plan.StateActive {
			it.active = append(it.active, *sid)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	countDoc := func(c plan.Count) map[string]any {
		return map[string]any{"done": c.Done, "total": c.Total, "manual": c.Manual, "stopped": c.Stopped, "state": c.State}
	}
	out := make([]any, 0, len(roots))
	for _, r := range roots {
		var all []plan.Step
		items := make([]any, 0, len(r.items))
		for _, it := range r.items {
			all = append(all, it.steps...)
			items = append(items, map[string]any{"plan_node_id": it.id, "label": it.label, "title": it.title,
				"count": countDoc(plan.CountSteps(it.steps)), "active_steps": nonNil(it.active)})
		}
		out = append(out, map[string]any{"plan_node_id": r.id, "title": r.title, "count": countDoc(plan.CountSteps(all)), "items": items})
	}
	return out, nil
}
