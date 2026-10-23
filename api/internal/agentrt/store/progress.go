package store

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
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
	StatusBlocked      = "blocked"
	StatusSlow         = "slow"
	StatusWorking      = "working"
	StatusWaitingRole  = "waiting_role"
	StatusQueued       = "queued"
)

var statusRank = []string{StatusHostLost, StatusMaybeStuck, StatusUnknown, StatusWaitingOwner, StatusBlocked, StatusSlow,
	StatusWorking, StatusWaitingRole, StatusQueued}

// HostLostAfter is how long since its last poll a host counts as lost: 30
// seconds (W-31 round 3, 3.5: 「主机掉线或执行者不可用，Console 和参谋长简报 1
// 分钟内说出来」; CUT-online-status-2026-10-23 option A). A host's claim polls
// about every second within its long poll, and agentd sleeps 15 seconds after
// a claim that failed, so 30 seconds holds one such failure (T24 had 3
// minutes).
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

	class       string
	stateReason string
	checkers    []string
}

// StatusInput is what a task's status is decided from.
type StatusInput struct {
	TaskState, MissionState, StateReason, MissionReason string
	Attempt                                             *ProgressAttempt
	Usual                                               *time.Duration
	WaitingDeps                                         int
	Now                                                 time.Time
}

// TaskStatus decides a task's status and its reason (SPEC §3.1). An attempt
// whose host the runtime does not know is unknown before anything else: what
// its host does cannot be told.
func TaskStatus(in StatusInput) (string, string) {
	if a := in.Attempt; a != nil {
		switch {
		case !a.hostKnown:
			return StatusUnknown, "the attempt names no host the runtime knows"
		case a.hostPolled == nil && a.hostLost(in.Now):
			return StatusHostLost, "host " + a.HostID + " has never polled" + notPersisted(a)
		case a.hostLost(in.Now):
			return StatusHostLost, "host " + a.HostID + " last polled " + agoWords(in.Now.Sub(*a.hostPolled)) + notPersisted(a)
		case a.State == "stale":
			return StatusMaybeStuck, "the lease lapsed and was not renewed" + notPersisted(a)
		}
	}
	switch {
	case in.TaskState == "awaiting_owner":
		return StatusWaitingOwner, "the task waits for the Owner"
	case in.MissionState == "proposed" || in.MissionState == "awaiting_owner":
		return StatusWaitingOwner, "the mission is " + in.MissionState + " and starts nothing until it is authorized"
	case in.TaskState == "blocked" || in.TaskState == "paused":
		reason := "the task is " + in.TaskState
		if in.StateReason != "" {
			reason += ": " + in.StateReason
		}
		return StatusBlocked, reason
	}
	if a := in.Attempt; a != nil {
		if in.Usual != nil && a.ElapsedS != nil && float64(*a.ElapsedS)*float64(time.Second) > SlowFactor*float64(*in.Usual) {
			return StatusSlow, fmt.Sprintf("running %d s, usually %d s", *a.ElapsedS, int64(*in.Usual/time.Second))
		}
		return StatusWorking, "attempt " + a.AttemptID + " on " + a.HostID
	}
	if in.MissionState == "paused" {
		reason := "the mission is paused"
		if in.MissionReason != "" {
			reason += ": " + in.MissionReason
		}
		return StatusBlocked, reason
	}
	switch in.TaskState {
	case "submitted", "verifying":
		return StatusWaitingRole, "waits for its verification or review"
	case "rework":
		return StatusWaitingRole, "waits for the next round of its implementation"
	case "proposed":
		if in.WaitingDeps > 0 {
			return StatusQueued, fmt.Sprintf("waits for %d dependencies to be accepted", in.WaitingDeps)
		}
		return StatusQueued, "waits for the controller to ready it"
	case "ready":
		return StatusQueued, "ready for a host to claim"
	}
	return StatusUnknown, "state " + in.TaskState + " has no status"
}

// hostLost says whether the attempt's host is lost at now: as the hosts'
// document judges it when read with it, else by its last contact alone.
func (a *ProgressAttempt) hostLost(now time.Time) bool {
	if a.hostSilent != nil {
		return *a.hostSilent
	}
	return a.hostPolled == nil || now.Sub(*a.hostPolled) > HostLostAfter
}

// agoWords is how long ago d was, in seconds under a minute and in whole
// minutes from then on.
func agoWords(d time.Duration) string {
	n, unit := int(d/time.Minute), "minute"
	if d < time.Minute {
		n, unit = int(d/time.Second), "second"
	}
	if n != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%d %s ago", n, unit)
}

// byMission says the mission's state, not the task's, decided status: the
// wait then started when the mission came into its state.
func byMission(status, taskState string) bool {
	switch status {
	case StatusWaitingOwner:
		return taskState != "awaiting_owner"
	case StatusBlocked:
		return taskState != "blocked" && taskState != "paused"
	}
	return false
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

// Progress computes the projection. A read that fails returns an error and
// nothing else.
func Progress(ctx context.Context, db DB) (map[string]any, error) {
	return progressAt(ctx, db, time.Time{}, time.Time{})
}

// ProgressSince is Progress for a reader that saw the platform take agentd's
// calls again at since: a host's silence counts from no earlier
// (presencelive.go, review 528 A1b).
func ProgressSince(ctx context.Context, db DB, since time.Time) (map[string]any, error) {
	return progressAt(ctx, db, time.Time{}, since)
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
	usual, err := usualTimes(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	live, err := liveAttempts(ctx, tx, now, since)
	if err != nil {
		return nil, err
	}
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
	// The hosts, and the executors and jobs presence judges from them
	// (presence.go), at the projection's moment.
	pr, err := readPresence(ctx, tx, now, since)
	if err != nil {
		return nil, err
	}
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
		"controller_alerts": alerts, "schema": schema, "executors": pr.executors, "jobs_without_executor": pr.unserved, "presence_unjudged": pr.unjudged, "presence_deferred": pr.deferred}, nil
}

// rawJSON is a document the database built, passed through as it is.
type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }

// usualTimes reads, per role and task class, the median time from start to
// submission of the attempts accepted in the window (SPEC §3.3); fewer than
// UsualMinSamples give none.
func usualTimes(ctx context.Context, tx pgx.Tx, now time.Time) (map[string]usualTime, error) {
	rows, err := tx.Query(ctx, `SELECT t.role || '|' || t.task_class,
	    percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM s.at - a.started_at)), count(*)
	  FROM agentrt.verification v
	  JOIN agentrt.attempt a ON a.attempt_id = v.attempt_id
	  JOIN agentrt.task t ON t.task_id = a.task_id
	  JOIN LATERAL (SELECT e.at FROM agentrt.event e WHERE e.task_id = a.task_id AND e.event_type = 'attempt_submitted'
	    AND e.attempt_id = a.attempt_id ORDER BY e.event_id LIMIT 1) s ON true
	  WHERE v.accepts_attempt AND a.started_at IS NOT NULL AND v.created_at > $1::timestamptz - make_interval(secs => $2)
	  GROUP BY 1`, now, usualWindow.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]usualTime{}
	for rows.Next() {
		var key string
		var median float64
		var n int
		if err = rows.Scan(&key, &median, &n); err != nil {
			return nil, err
		}
		if n >= UsualMinSamples {
			out[key] = usualTime{d: time.Duration(median * float64(time.Second)), n: n}
		}
	}
	return out, rows.Err()
}

func liveAttempts(ctx context.Context, tx pgx.Tx, now, since time.Time) (map[string]*ProgressAttempt, error) {
	rows, err := tx.Query(ctx, `SELECT a.attempt_id, a.attempt_no, a.task_id, t.mission_id, COALESCE(a.host_id, ''), h.host_id IS NOT NULL,
	    h.last_poll_at, `+hostSilentSQL("$4::timestamptz")+`, a.executor_actor_id, COALESCE(a.model, ''), a.state, a.started_at,
	    a.heartbeat_at, t.role, t.task_class, t.state, a.persisted_at IS NOT NULL, cp.seq, cp.at
	  FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
	  LEFT JOIN agentrt.host h ON h.host_id = a.host_id `+hostLiveJoins+`
	  `+checkpointOf+`
	  WHERE a.state IN ('leased','running','stale')`, append(livenessArgs(since), now)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ProgressAttempt{}
	for rows.Next() {
		a := &ProgressAttempt{}
		var started, checkpoint *time.Time
		var heartbeat time.Time
		var persisted bool
		var seq *int64
		if err = rows.Scan(&a.AttemptID, &a.AttemptNo, &a.TaskID, &a.MissionID, &a.HostID, &a.hostKnown, &a.hostPolled, &a.hostSilent, &a.Executor,
			&a.Model, &a.State, &started, &heartbeat, &a.role, &a.class, &a.taskState, &persisted, &seq, &checkpoint); err != nil {
			return nil, err
		}
		if started != nil {
			a.started = *started
			a.StartedAt = started.UTC().Format(time.RFC3339Nano)
			e := int64(now.Sub(*started) / time.Second)
			a.ElapsedS = &e
		}
		a.HeartbeatAgeS = int64(now.Sub(heartbeat) / time.Second)
		a.keep(persisted, seq, checkpoint, now)
		out[a.TaskID] = a
	}
	return out, rows.Err()
}

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

func usualOf(usual map[string]usualTime, role, class string) *time.Duration {
	if u, ok := usual[role+"|"+class]; ok {
		return &u.d
	}
	return nil
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
	lost, err := lostAttempts(ctx, tx, ids, now)
	if err != nil {
		return nil, nil, err
	}
	// The unfinished tasks, with when they came into their state (the event,
	// not updated_at, which a plan migration moves), why, and the roles of
	// the unfinished verifications and reviews of them.
	trows, err := tx.Query(ctx, `SELECT t.task_id, t.mission_id, t.title, t.role, COALESCE(t.plan_node_id, ''), t.off_plan,
	    COALESCE(t.off_plan_reason, ''), t.round_no, t.max_rounds, t.state, t.task_class, s.at, COALESCE(s.body_json->>'reason', ''),
	    ARRAY(SELECT d.depends_on_task_id FROM agentrt.task_dependency d JOIN agentrt.task o ON o.task_id = d.depends_on_task_id
	      WHERE d.task_id = t.task_id AND o.state <> 'accepted' ORDER BY d.depends_on_task_id),
	    COALESCE((SELECT a.computed_at IS NOT NULL AND a.persisted_at IS NULL FROM agentrt.attempt a WHERE a.task_id = t.task_id
	      ORDER BY a.attempt_no DESC LIMIT 1), false),
	    ARRAY(SELECT DISTINCT c.role FROM agentrt.task c WHERE c.subject_task_id = t.task_id
	      AND c.state NOT IN ('accepted','rejected','cancelled') ORDER BY c.role)
	  FROM agentrt.task t
	  LEFT JOIN LATERAL (SELECT e.at, e.body_json FROM agentrt.event e WHERE e.task_id = t.task_id
	    AND e.event_type IN ('task_proposed','task_state_changed') ORDER BY e.event_id DESC LIMIT 1) s ON true
	  WHERE t.mission_id = ANY($1) AND t.state NOT IN ('accepted','rejected','cancelled')
	  ORDER BY t.created_at, t.task_id`, ids)
	if err != nil {
		return nil, nil, err
	}
	defer trows.Close()
	for trows.Next() {
		t := &ProgressTask{}
		var missionID string
		var since *time.Time
		if err = trows.Scan(&t.TaskID, &missionID, &t.Title, &t.Role, &t.PlanNodeID, &t.OffPlan, &t.OffPlanReason, &t.RoundNo,
			&t.MaxRounds, &t.State, &t.class, &since, &t.stateReason, &t.DependsOn, &t.ComputedNotPersisted, &t.checkers); err != nil {
			return nil, nil, err
		}
		m := byID[missionID]
		t.Since = stamp(since)
		t.Attempt, t.LostAttempt = live[t.TaskID], lost[t.TaskID]
		u := usualOf(usual, t.Role, t.class)
		if u != nil {
			s := int64(*u / time.Second)
			t.UsualS = &s
		}
		t.Status, t.Reason = TaskStatus(StatusInput{TaskState: t.State, MissionState: m.state, StateReason: t.stateReason,
			MissionReason: m.stateReason, Attempt: t.Attempt, Usual: u, WaitingDeps: len(t.DependsOn), Now: now})
		if byMission(t.Status, t.State) {
			t.Since = m.stateAt
		}
		if t.Status == StatusWaitingRole {
			switch {
			case t.State == "rework":
				t.WaitingRoles = []string{t.Role}
			case len(t.checkers) > 0:
				t.WaitingRoles = t.checkers
			default:
				t.Reason += "; none is proposed yet"
			}
		}
		if t.Attempt != nil {
			t.Attempt.Status, t.Attempt.Reason = t.Status, t.Reason
		} else if l := t.LostAttempt; l != nil && !l.Persisted {
			t.Reason += "; " + l.Reason
		}
		m.tasks = append(m.tasks, t)
	}
	if err = trows.Err(); err != nil {
		return nil, nil, err
	}
	// A live attempt of a mission not listed still gets its status.
	for _, a := range live {
		if a.Status == "" {
			a.Status, a.Reason = TaskStatus(StatusInput{TaskState: a.taskState, Attempt: a, Usual: usualOf(usual, a.role, a.class), Now: now})
		}
	}
	list := make([]any, 0, len(missions))
	etas := make(map[string]map[string]any, len(missions))
	for _, m := range missions {
		etas[m.id] = missionETA(m, usual, now)
		list = append(list, missionDoc(m, etas[m.id]))
	}
	return map[string]any{"missions": list, "truncated": truncated}, ownerNext(missions, etas, truncated, now), nil
}

// missionDoc is one mission with its status (SPEC §3.2) and when it should
// be done (progresseta.go).
func missionDoc(m *progressMission, eta map[string]any) map[string]any {
	statuses := make([]string, 0, len(m.tasks))
	tasks := make([]any, 0, len(m.tasks))
	for _, t := range m.tasks {
		statuses = append(statuses, t.Status)
		tasks = append(tasks, t)
	}
	count := plan.CountSteps(m.steps)
	status, reason, since, waiting := worst(statuses), "", "", "none"
	var roles []string
	if status != "" {
		for _, t := range m.tasks {
			if t.Status == status {
				reason, since, roles = t.Reason, t.Since, t.WaitingRoles
				break
			}
		}
		// A mission on the Owner's card says so, with or without tasks
		// (review 190 B4).
		if status == StatusWaitingOwner && m.state == "awaiting_owner" && m.stateQuestion != "" {
			reason = missionCardReason(m)
		}
		switch status {
		case StatusWaitingOwner:
			waiting = "owner"
		case StatusWaitingRole:
			waiting = "role"
			if len(roles) == 0 {
				waiting = "coordinator"
			}
		case StatusHostLost, StatusMaybeStuck, StatusUnknown, StatusBlocked, StatusQueued:
			waiting = "coordinator"
		}
	} else {
		switch {
		case m.state == "proposed" || m.state == "awaiting_owner":
			status, waiting, since = StatusWaitingOwner, "owner", m.stateAt
			reason = "the mission is " + m.state + ": it waits for a signed standing-envelope rule, which nothing the Owner can click gives yet"
			if m.stateQuestion != "" {
				reason = missionCardReason(m)
			}
		case m.state == "paused":
			status, waiting, since = StatusBlocked, "coordinator", m.stateAt
			reason = "the mission is paused"
			if m.stateBy != "" {
				reason += " by " + m.stateBy
			}
			if m.stateReason != "" {
				reason += ": " + m.stateReason
			}
		case count.Total > count.Done:
			status, waiting, reason, since = "idle", "coordinator", "no task proposed for the next step", m.lastAt
		default:
			status, reason, since = "done", "every step is done", m.lastAt
		}
	}
	return map[string]any{"mission_id": m.id, "title": m.title, "goal": m.goal, "state": m.state, "deadline_at": m.deadline,
		"work_id": m.work, "slices": countDoc(count), "status": status, "reason": reason, "since": since, "waiting_on": waiting,
		"waiting_roles": nonNil(roles), "tasks": tasks, "next_steps": nonNilAny(m.next),
		"eta": eta}
}

// missionCardReason is why a mission on the Owner's card waits.
func missionCardReason(m *progressMission) string {
	return "the mission waits for the Owner's answer on question " + m.stateQuestion + ": " + m.stateReason
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
	rows, err := tx.Query(ctx, `SELECT r.plan_node_id, r.node_kind, r.title, i.plan_node_id, i.label, i.title,
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
		id, kind, title string
		items           []*item
	}
	var roots []*root
	byRoot := map[string]*root{}
	byItem := map[string]*item{}
	for rows.Next() {
		var rid, rkind, rtitle string
		var iid, ilabel, ititle, sid, sstate *string
		var cause string
		if err = rows.Scan(&rid, &rkind, &rtitle, &iid, &ilabel, &ititle, &sid, &sstate, &cause); err != nil {
			return nil, err
		}
		r := byRoot[rid]
		if r == nil {
			r = &root{id: rid, kind: rkind, title: rtitle}
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
	rows.Close()
	deploys := newRecordedDeploys(ctx, tx)
	// One reading of each gate for the whole projection, every root's rows (review 427 B2, 429 C).
	gates := map[string]plan.Tri{}
	out := make([]any, 0, len(roots))
	for _, r := range roots {
		doc := map[string]any{"plan_node_id": r.id, "kind": r.kind, "title": r.title}
		if r.kind == "arch_root" {
			arch := make([]archRow, 0, len(r.items))
			for _, it := range r.items {
				arch = append(arch, archRow{id: it.id, title: it.title})
			}
			if doc["rows"], err = archRowsDoc(ctx, tx, arch, deploys, gates); err != nil {
				return nil, err
			}
			out = append(out, doc)
			continue
		}
		var all []plan.Step
		items := make([]any, 0, len(r.items))
		for _, it := range r.items {
			all = append(all, it.steps...)
			items = append(items, map[string]any{"plan_node_id": it.id, "label": it.label, "title": it.title,
				"count": countDoc(plan.CountSteps(it.steps)), "active_steps": nonNil(it.active)})
		}
		doc["count"], doc["items"] = countDoc(plan.CountSteps(all)), items
		out = append(out, doc)
	}
	return out, nil
}

// recentView is the last ended tasks, by the event that ended them, and the
// last failed attempts, whatever came of their tasks (SPEC §2.6, T06). Both
// read the events newest first through the (event_type, at) index, and break
// ties of time by event id at every step, so the same events always give the
// same lists.
func recentView(ctx context.Context, tx pgx.Tx) (map[string]any, error) {
	var tasks, failed []byte
	err := tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(r ORDER BY r.ended_at DESC, r.ended_event_id DESC), '[]'::jsonb) FROM (
	    SELECT t.task_id, t.mission_id, t.title, t.role, t.state, e.at AS ended_at, e.event_id AS ended_event_id, a.conclusion,
	      a.report_artifact_id
	    FROM (SELECT DISTINCT ON (task_id) task_id, at, event_id FROM (
	        (SELECT task_id, at, event_id FROM agentrt.event WHERE event_type = 'task_state_changed'
	          AND body_json->>'to' IN ('accepted','rejected','cancelled') ORDER BY at DESC, event_id DESC LIMIT $1 * 2)
	        UNION ALL
	        (SELECT task_id, at, event_id FROM agentrt.event WHERE event_type = 'task_cancelled' ORDER BY at DESC, event_id DESC LIMIT $1 * 2)) x
	      ORDER BY task_id, at DESC, event_id DESC) e
	    JOIN agentrt.task t ON t.task_id = e.task_id AND t.state IN ('accepted','rejected','cancelled')
	    LEFT JOIN LATERAL (SELECT conclusion, report_artifact_id FROM agentrt.attempt WHERE task_id = t.task_id
	      ORDER BY attempt_no DESC LIMIT 1) a ON true
	    ORDER BY e.at DESC, e.event_id DESC LIMIT $1) r`, progressRecent).Scan(&tasks)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(r ORDER BY r.ended_at DESC, r.ended_event_id DESC), '[]'::jsonb) FROM (
	    SELECT a.attempt_id, a.task_id, t.mission_id, e.at AS ended_at, e.event_id AS ended_event_id, a.report_artifact_id
	    FROM agentrt.event e JOIN agentrt.attempt a ON a.attempt_id = e.attempt_id JOIN agentrt.task t ON t.task_id = a.task_id
	    WHERE e.event_type = 'attempt_computed' AND e.body_json->>'conclusion' = 'failed'
	    ORDER BY e.at DESC, e.event_id DESC LIMIT $1) r`, progressRecent).Scan(&failed)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": rawJSON(tasks), "failed_attempts": rawJSON(failed)}, nil
}
