package store

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
)

// The progress projection (GET /progress; the interface is fixed in
// SPEC-agentrt-progress-2026-10-14.md under agent-config/work/multi-agent,
// second version): computed on every read from the runtime's own rows, in one
// read-only snapshot at one moment, never stored. What cannot be computed is
// unknown with its reason, never a value that looks normal. The lists the spec
// bounds are bounded; the other reads are sized for the runtime's scale, and
// the spec's §4 says up to what size and who measures them before 2.5.

// Task statuses, in the order a mission shows the one most worth seeing.
const (
	StatusHostLost     = "host_lost"
	StatusMaybeStuck   = "maybe_stuck"
	StatusUnknown      = "unknown"
	StatusWaitingOwner = "waiting_owner"
	StatusStopped      = "stopped" // only the Owner lifts a stop (progressstop.go)
	StatusBlocked      = "blocked"
	StatusSlow         = "slow"
	StatusWorking      = "working"
	StatusWaitingRole  = "waiting_role"
	StatusQueued       = "queued"
	// A task whose change is on main (progressstop.go), whatever its state:
	// finished, as an accepted task is, so not ranked among the open ones.
	StatusFinished = "finished"
)

var statusRank = []string{StatusHostLost, StatusMaybeStuck, StatusUnknown, StatusWaitingOwner, StatusStopped, StatusBlocked,
	StatusSlow, StatusWorking, StatusWaitingRole, StatusQueued}

// HostLostAfter retains the historical display threshold. The progress
// projection now returns no dispatch hosts or live attempts.
const HostLostAfter = 30 * time.Second

// SlowFactor: a working attempt is slow past this many times the usual.
const SlowFactor = 1.5

// The history a usual time is read from: fewer samples give none.
const (
	UsualMinSamples = 3
	usualWindow     = 30 * 24 * time.Hour
)

// Bounds of the projection's lists.
const (
	progressMissions = 100
	progressRecent   = 20
	// A plan change can wait for the Owner no longer than its card lives
	// (at most 7 days) and a decision pass after it.
	pendingChangeWindow = 8 * 24 * time.Hour
)

// pendingChangeLimit is the most plan changes needs_you lists (a test lowers
// it).
var pendingChangeLimit = 50

// ProgressAttempt is a live attempt.
type ProgressAttempt struct {
	AttemptID     string `json:"attempt_id"`
	AttemptNo     int    `json:"attempt_no"`
	TaskID        string `json:"task_id"`
	MissionID     string `json:"mission_id"`
	HostID        string `json:"host_id,omitempty"`
	Executor      string `json:"executor"`
	Model         string `json:"model,omitempty"`
	State         string `json:"state"`
	StartedAt     string `json:"started_at,omitempty"`
	ElapsedS      *int64 `json:"elapsed_s,omitempty"`
	HeartbeatAgeS int64  `json:"heartbeat_age_s"`
	Status        string `json:"status,omitempty"`
	Reason        string `json:"reason"`
	// What of its work is persisted (TD-352, progresspersist.go); a lost
	// attempt also says when it ended.
	Persisted         bool   `json:"persisted"`
	LastCheckpointSeq *int64 `json:"last_checkpoint_seq,omitempty"`
	LastCheckpointAt  string `json:"last_checkpoint_at,omitempty"`
	UnpersistedForS   *int64 `json:"unpersisted_for_s,omitempty"`
	EndedAt           string `json:"ended_at,omitempty"`
	started           time.Time
	hostKnown         bool
	hostPolled        *time.Time
	hostSilent        *bool // as HostsSQL has it (presencelive.go); nil: judged from hostPolled alone
	role, class       string
	taskState         string
}

// ProgressTask is an unfinished task of a mission.
type ProgressTask struct {
	TaskID               string           `json:"task_id"`
	Title                string           `json:"title"`
	Role                 string           `json:"role"`
	PlanNodeID           string           `json:"plan_node_id,omitempty"`
	OffPlan              bool             `json:"off_plan"`
	OffPlanReason        string           `json:"off_plan_reason,omitempty"`
	RoundNo              int              `json:"round_no"`
	MaxRounds            int              `json:"max_rounds"`
	State                string           `json:"state"`
	Status               string           `json:"status"`
	Reason               string           `json:"reason"`
	Since                string           `json:"since,omitempty"`
	DependsOn            []string         `json:"depends_on"`
	WaitingRoles         []string         `json:"waiting_roles,omitempty"`
	Attempt              *ProgressAttempt `json:"attempt,omitempty"`
	UsualS               *int64           `json:"usual_s,omitempty"`
	ComputedNotPersisted bool             `json:"computed_not_persisted"`
	LostAttempt          *ProgressAttempt `json:"lost_attempt,omitempty"` // its latest lost attempt no later result superseded
	Stopped              *ProgressStop    `json:"stopped,omitempty"`
	OnMain               *ProgressOnMain  `json:"on_main,omitempty"`

	class       string
	stateReason string
	checkers    []string
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

// ProgressSince retains the progress reader signature; dispatch host
// presence is no longer computed in this projection.
func ProgressSince(ctx context.Context, db DB, since time.Time) (map[string]any, error) {
	return progressAt(ctx, db, time.Time{}, since)
}

// Progress computes the projection. A read that fails returns an error and
// nothing else.
func Progress(ctx context.Context, db DB) (map[string]any, error) {
	return progressAt(ctx, db, time.Time{}, time.Time{})
}

// progressAt is the projection at at (zero: the database's clock).
func progressAt(ctx context.Context, db DB, at, since time.Time) (map[string]any, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	var asOf int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE($1, clock_timestamp()), COALESCE((SELECT max(event_id) FROM agentrt.event), 0)`,
		nullTime(at)).Scan(&now, &asOf); err != nil {
		return nil, err
	}
	// Nothing runs a task any more (store cut 4, 2026-11-14): no attempt is
	// live and no usual time is learned. The document keeps its shape; these
	// parts are empty.
	usual, live := map[string]usualTime{}, map[string]*ProgressAttempt{}
	work, next, err := missionsInWork(ctx, tx, now, live, usual)
	if err != nil {
		return nil, err
	}
	needs, err := needsYou(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	planDoc, err := planView(ctx, tx)
	if err != nil {
		return nil, err
	}
	// The dispatch hosts are retired (2026-11-15): nothing reads agentrt.host
	// here any more. The key stays on the wire, as an empty list, for a
	// Console from before this.
	pr := struct{ doc []byte }{doc: []byte("[]")}
	recent, err := recentView(ctx, tx)
	if err != nil {
		return nil, err
	}
	var controls []byte
	if err = tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.set_at DESC), '[]'::jsonb)
	  FROM agentrt.control c WHERE c.cleared_at IS NULL`).Scan(&controls); err != nil {
		return nil, err
	}
	liveDoc := make([]any, 0, len(live))
	for _, a := range sortedLive(live) {
		liveDoc = append(liveDoc, a)
	}
	alerts, schema, err := controllerState(ctx, tx) // last: progressschema.go
	if err != nil {
		return nil, err
	}
	return map[string]any{"generated_at": now.UTC().Format(time.RFC3339Nano), "as_of_event_id": asOf, "needs_you": needs,
		"work": work, "owner_next": next, "live": liveDoc, "plan": planDoc, "hosts": rawJSON(pr.doc), "controls": rawJSON(controls), "recent": recent,
		"controller_alerts": alerts, "schema": schema}, nil
}

// rawJSON is a document the database built, passed through as it is.
type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }

// sortedLive orders live attempts by start time (an attempt not started yet
// last), then id.
func sortedLive(live map[string]*ProgressAttempt) []*ProgressAttempt {
	list := make([]*ProgressAttempt, 0, len(live))
	for _, a := range live {
		list = append(list, a)
	}
	slices.SortFunc(list, func(x, y *ProgressAttempt) int {
		switch {
		case x.started.IsZero() != y.started.IsZero():
			if x.started.IsZero() {
				return 1
			}
			return -1
		case !x.started.Equal(y.started):
			return x.started.Compare(y.started)
		}
		if x.AttemptID < y.AttemptID {
			return -1
		}
		return 1
	})
	return list
}

// usualTime is a role and task class's usual time and how many accepted
// attempts it is the median of.
type usualTime struct {
	d time.Duration
	n int
}

// countDoc is a count by the counting rule, as JSON.
func countDoc(c plan.Count) map[string]any {
	return map[string]any{"done": c.Done, "total": c.Total, "manual": c.Manual, "stopped": c.Stopped, "state": c.State}
}

type progressMission struct {
	id, title, goal, state, deadline, work string
	// The event that put the mission in its state (when, why, by whom) and
	// the mission's last event (when anything last happened in it).
	stateAt, stateReason, stateBy, stateQuestion, lastAt string
	steps                                                []plan.Step
	next                                                 []any
	tasks                                                []*ProgressTask
	// Its tasks whose change is on main: listed, finished, never open work.
	onMain []*ProgressTask
}

func stamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// missionsInWork is the open missions, and when the Owner is next needed
// after what waits for him now (progressnext.go), from the same etas.
func missionsInWork(ctx context.Context, tx pgx.Tx, now time.Time, live map[string]*ProgressAttempt, usual map[string]usualTime) (map[string]any, map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT m.mission_id, m.title, m.goal, m.state, m.deadline_at, COALESCE(m.work_id, ''),
	    s.at, COALESCE(s.body_json->>'reason', ''), COALESCE(s.actor_id, ''), COALESCE(s.body_json->>'question_id', ''), l.at
	  FROM agentrt.mission m
	  LEFT JOIN LATERAL (SELECT e.at, e.body_json, e.actor_id FROM agentrt.event e WHERE e.mission_id = m.mission_id
	    AND e.event_type IN ('mission_proposed','mission_state_changed') ORDER BY e.event_id DESC LIMIT 1) s ON true
	  LEFT JOIN LATERAL (SELECT e.at FROM agentrt.event e WHERE e.mission_id = m.mission_id ORDER BY e.event_id DESC LIMIT 1) l ON true
	  WHERE m.state IN ('proposed','awaiting_owner','active','paused') ORDER BY substr(m.mission_id, 3)::int LIMIT $1`, progressMissions+1)
	if err != nil {
		return nil, nil, err
	}
	var missions []*progressMission
	byID := map[string]*progressMission{}
	for rows.Next() {
		m := &progressMission{}
		var deadline time.Time
		var stateAt, lastAt *time.Time
		if err = rows.Scan(&m.id, &m.title, &m.goal, &m.state, &deadline, &m.work, &stateAt, &m.stateReason, &m.stateBy,
			&m.stateQuestion, &lastAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		m.deadline = deadline.UTC().Format(time.RFC3339)
		m.stateAt, m.lastAt = stamp(stateAt), stamp(lastAt)
		missions = append(missions, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	truncated := len(missions) > progressMissions
	if truncated {
		missions = missions[:progressMissions]
	}
	ids := make([]string, 0, len(missions))
	for _, m := range missions {
		byID[m.id] = m
		ids = append(ids, m.id)
	}
	// The slices: the steps of each mission, by the counting rule, and the
	// next steps not started, in plan order.
	srows, err := tx.Query(ctx, `SELECT s.mission_id, s.plan_node_id, s.title, s.state, COALESCE(e.body_json->>'cause', ''), s.owner_step
	  FROM agentrt.plan_node s JOIN agentrt.plan_node i ON i.plan_node_id = s.parent_plan_node_id
	  LEFT JOIN agentrt.event e ON e.event_id = s.state_event_id
	  WHERE s.node_kind = 'step' AND s.mission_id = ANY($1)
	  ORDER BY i.ordinal, s.ordinal, s.plan_node_id`, ids)
	if err != nil {
		return nil, nil, err
	}
	for srows.Next() {
		var mission, id, title, state, cause string
		var owner bool
		if err = srows.Scan(&mission, &id, &title, &state, &cause, &owner); err != nil {
			srows.Close()
			return nil, nil, err
		}
		m := byID[mission]
		m.steps = append(m.steps, plan.Step{ID: id, State: state, Manual: state == plan.StateDone && cause == "done_rule_manual"})
		if state == plan.StatePlanned && len(m.next) < 2 {
			m.next = append(m.next, map[string]any{"plan_node_id": id, "title": title, "owner_step": owner})
		}
	}
	srows.Close()
	if err = srows.Err(); err != nil {
		return nil, nil, err
	}
	// A mission's tasks are not read any more: it has none in work.
	list := make([]any, 0, len(missions))
	etas := make(map[string]map[string]any, len(missions))
	for _, m := range missions {
		etas[m.id] = missionETA(m, usual, now)
		list = append(list, missionDoc(m, etas[m.id]))
	}
	return map[string]any{"missions": list, "truncated": truncated}, ownerNext(missions, etas, truncated, now), nil
}

// missionDoc retains the progress reader contract; the Plan projection receives
// data only. Ranking, counting and evidence decisions remain in their existing callers.
func missionDoc(m *progressMission, eta map[string]any) map[string]any {
	statuses := make([]string, 0, len(m.tasks))
	tasks := make([]planreadings.MissionTaskProjection, 0, len(m.tasks))
	for _, t := range m.tasks {
		statuses = append(statuses, t.Status)
		tasks = append(tasks, planreadings.MissionTaskProjection{
			Status: t.Status, Reason: t.Reason, Since: t.Since, WaitingRoles: t.WaitingRoles, Document: t,
		})
	}
	onMain := make([]any, 0, len(m.onMain))
	for _, t := range m.onMain {
		onMain = append(onMain, t)
	}
	return planreadings.MissionDocument(planreadings.MissionProjection{
		ID: m.id, Title: m.title, Goal: m.goal, State: m.state, Deadline: m.deadline, Work: m.work,
		StateAt: m.stateAt, StateReason: m.stateReason, StateBy: m.stateBy, StateQuestion: m.stateQuestion, LastAt: m.lastAt,
		Count: plan.CountSteps(m.steps), SelectedStatus: worst(statuses), Tasks: tasks, OnMain: onMain, Next: m.next,
	}, eta)
}

func nonNilAny(s []any) []any {
	if s == nil {
		return []any{}
	}
	return s
}

// planView is each round with its count and its items' counts, and each
// architecture root with its rows, each ticked by its done rule evaluated
// now and with its latest note (SPEC §2.4; progressarch.go).
func planView(ctx context.Context, tx pgx.Tx) ([]any, error) {
	roots, err := planreadings.ProgressTree(ctx, tx)
	if err != nil {
		return nil, err
	}
	deploys := newRecordedDeploys(ctx, tx)
	// One reading of each gate for the whole projection, every root's rows (review 427 B2, 429 C).
	gates := map[string]plan.Tri{}
	out := make([]any, 0, len(roots))
	for _, r := range roots {
		doc := map[string]any{"plan_node_id": r.ID, "kind": r.Kind, "title": r.Title}
		if r.Kind == "arch_root" {
			arch := make([]archRow, 0, len(r.Items))
			for _, it := range r.Items {
				arch = append(arch, archRow{id: it.ID, title: it.Title})
			}
			if doc["rows"], err = archRowsDoc(ctx, tx, arch, deploys, gates); err != nil {
				return nil, err
			}
			out = append(out, doc)
			continue
		}
		var all []plan.Step
		items := make([]any, 0, len(r.Items))
		for _, it := range r.Items {
			all = append(all, it.Steps...)
			items = append(items, map[string]any{"plan_node_id": it.ID, "label": it.Label, "title": it.Title,
				"count": countDoc(plan.CountSteps(it.Steps)), "active_steps": nonNil(it.Active)})
		}
		doc["count"], doc["items"] = countDoc(plan.CountSteps(all)), items
		out = append(out, doc)
	}
	return out, nil
}
