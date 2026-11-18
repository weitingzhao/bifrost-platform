package planstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
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
	statusBlocked      = "blocked"
	StatusSlow         = "slow"
	StatusWorking      = "working"
	StatusWaitingRole  = "waiting_role"
	StatusQueued       = "queued"
	// A task whose change is on main (progressstop.go), whatever its state:
	// finished, as an accepted task is, so not ranked among the open ones.
	StatusFinished = "finished"
)

// HostLostAfter retains the historical display threshold. The progress
// projection now returns no dispatch hosts or live attempts.
const HostLostAfter = 30 * time.Second

// The history a usual time is read from: fewer samples give none.
const (
	UsualMinSamples = 3
)

// Bounds of the projection's lists.
const (
	progressMissions = 100
	// A plan change can wait for the Owner no longer than its card lives
	// (at most 7 days) and a decision pass after it.
	pendingChangeWindow = 8 * 24 * time.Hour
)

// PendingChangeLimit is the most plan changes needs_you lists (a test lowers
// it).
var PendingChangeLimit = 50

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
func worst(statuses []string) string { return planreadings.MostRelevantStatus(statuses) }

// ProgressSince retains the progress reader signature; dispatch host
// presence is no longer computed in this projection.
func ProgressSince(ctx context.Context, db eventlog.DB, since time.Time) (map[string]any, error) {
	return progressAt(ctx, db, time.Time{}, since)
}

// Progress computes the projection. A read that fails returns an error and
// nothing else.
func Progress(ctx context.Context, db eventlog.DB) (map[string]any, error) {
	return progressAt(ctx, db, time.Time{}, time.Time{})
}

// progressAt is the projection at at (zero: the database's clock).
func progressAt(ctx context.Context, db eventlog.DB, at, since time.Time) (map[string]any, error) {
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
	return planreadings.OrderLiveAttempts(live, func(a *ProgressAttempt) (time.Time, string) { return a.started, a.AttemptID })
}

// usualTime is a role and task class's usual time and how many accepted
// attempts it is the median of.
type usualTime struct {
	d time.Duration
	n int
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

// missionsInWork is the open missions, and when the Owner is next needed
// after what waits for him now (progressnext.go), from the same etas.
func missionsInWork(ctx context.Context, tx pgx.Tx, now time.Time, live map[string]*ProgressAttempt, usual map[string]usualTime) (map[string]any, map[string]any, error) {
	observed, truncated, err := planreadings.WorkMissions(ctx, tx, progressMissions)
	if err != nil {
		return nil, nil, err
	}
	missions := make([]*progressMission, 0, len(observed))
	for _, m := range observed {
		missions = append(missions, &progressMission{id: m.ID, title: m.Title, goal: m.Goal, state: m.State, deadline: m.Deadline, work: m.Work, stateAt: m.StateAt, stateReason: m.StateReason, stateBy: m.StateBy, stateQuestion: m.StateQuestion, lastAt: m.LastAt, steps: m.Steps, next: m.Next})
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
	return planreadings.ProjectTree(roots, func(items []*planreadings.TreeItem) (any, error) {
		arch := make([]archRow, 0, len(items))
		for _, it := range items {
			arch = append(arch, archRow{id: it.ID, title: it.Title})
		}
		return archRowsDoc(ctx, tx, arch, deploys, gates)
	})
}
