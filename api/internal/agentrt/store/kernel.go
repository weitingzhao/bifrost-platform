package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// The kernel: the state machine of missions, tasks and attempts that the
// HTTP routes of design v5 §5.2 call. Every change is an event plus the row
// it names, in one transaction; missions, tasks and steps move only along
// their transition tables (T25).
//
// Lock order, everywhere: attempt, then task, then mission, then the event
// stream (Append). Claims start at the task and never lock an existing
// attempt; a candidate a claim does not take is released at once.

// Lease timing (v5 §5.2, §6.2): a lease lasts LeaseTTL from the last
// heartbeat; once it lapses the attempt is stale and its host may still renew
// it within Grace; after that it is lost.
const (
	LeaseTTL = 120 * time.Second
	Grace    = 15 * time.Minute
)

var (
	// ErrFenced: the caller does not hold this attempt (another host, an old
	// epoch, an attempt that is no longer live, or past expiry plus grace).
	ErrFenced = errors.New("store: this attempt's lease is not yours")
	// ErrConflict: an idempotency key reused with a different request.
	ErrConflict = errors.New("store: idempotency key reused with a different request")
	// ErrInGrace: salvage refused because the lease may still be renewed.
	ErrInGrace = errors.New("store: the attempt is still within its grace period")
	// ErrState: the operation does not fit the current state.
	ErrState = errors.New("store: not allowed in the current state")
)

// DB is what the kernel needs: a pool or a connection.
type DB interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// testHook, set only by tests, runs at named points so a test can hold a
// call there and observe or race it.
var testHook func(point, arg string)

func hook(point, arg string) {
	if testHook != nil {
		testHook(point, arg)
	}
}

func timeText(t time.Time) string { return t.UTC().Truncate(time.Microsecond).Format(timeLayout) }

// AttemptRef is the only branch an attempt may push to.
func AttemptRef(taskID string, n int) string {
	return fmt.Sprintf("refs/heads/agent/%s/a%d", taskID, n)
}

// Missions.

var missionMoves = map[string][]string{
	"proposed":       {"awaiting_owner", "active", "stopped"},
	"awaiting_owner": {"active", "stopped"},
	"active":         {"paused", "done", "stopped", "expired"},
	"paused":         {"active", "stopped", "expired"},
}

// MoveMission changes a mission's state with mission_state_changed. It holds
// the mission row exclusively, so a claim of one of its tasks either commits
// first or sees the new state.
func MoveMission(ctx context.Context, tx pgx.Tx, actor, missionID, to, reason string) error {
	var from string
	if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`, missionID).Scan(&from); err != nil {
		return err
	}
	if !slices.Contains(missionMoves[from], to) {
		return fmt.Errorf("%w: mission %s %s -> %s", ErrState, missionID, from, to)
	}
	body := map[string]any{"from": from, "to": to}
	if reason != "" {
		body["reason"] = reason
	}
	if _, err := Append(ctx, tx, Event{Stream: MissionStream(missionID), Type: "mission_state_changed", Actor: actor,
		MissionID: missionID, Body: body}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET state = $2, version = version + 1 WHERE mission_id = $1`, missionID, to)
	return err
}

// Tasks.

// taskMoves is every task state change there is. The attempt routes below
// make the moves into and out of leased, running and submitted; verification
// makes the moves out of submitted and verifying; MoveTask makes the rest.
var taskMoves = map[string][]string{
	"proposed":       {"ready", "awaiting_owner", "blocked", "cancelled"},
	"awaiting_owner": {"ready", "cancelled"},
	"ready":          {"leased", "paused", "blocked", "cancelled"},
	"leased":         {"running", "ready", "blocked"},
	"running":        {"submitted", "ready", "blocked"},
	"submitted":      {"verifying", "blocked"},
	"verifying":      {"accepted", "rework", "rejected", "blocked"},
	"rework":         {"ready", "cancelled"},
	"blocked":        {"ready", "cancelled"},
	"paused":         {"ready", "cancelled"},
}

// controllerMoves are the moves MoveTask accepts: none of them touches a task
// that has a live attempt or a result under verification.
var controllerMoves = map[string][]string{
	"proposed":       {"ready", "awaiting_owner", "blocked", "cancelled"},
	"awaiting_owner": {"ready", "cancelled"},
	"ready":          {"paused", "blocked", "cancelled"},
	"rework":         {"ready", "cancelled"},
	"blocked":        {"ready", "cancelled"},
	"paused":         {"ready", "cancelled"},
}

type taskRow struct {
	id, mission, step, state, effect, role, class string
}

func readTask(ctx context.Context, tx pgx.Tx, id string, lock bool) (taskRow, error) {
	q := `SELECT task_id, mission_id, COALESCE(plan_node_id, ''), state, effect_class, role, task_class FROM agentrt.task WHERE task_id = $1`
	if lock {
		q += ` FOR UPDATE`
	}
	var t taskRow
	err := tx.QueryRow(ctx, q, id).Scan(&t.id, &t.mission, &t.step, &t.state, &t.effect, &t.role, &t.class)
	return t, err
}

// moveTask writes task_state_changed and the new state. t must be locked. A
// task that becomes ready wakes the hosts.
func moveTask(ctx context.Context, tx pgx.Tx, actor string, t taskRow, to, reason string) error {
	if !slices.Contains(taskMoves[t.state], to) {
		return fmt.Errorf("%w: task %s %s -> %s", ErrState, t.id, t.state, to)
	}
	body := map[string]any{"from": t.state, "to": to}
	if reason != "" {
		body["reason"] = reason
	}
	ev, err := Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "task_state_changed", Actor: actor,
		MissionID: t.mission, PlanNode: t.step, TaskID: t.id, Body: body})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = $2, version = version + 1, updated_at = clock_timestamp() WHERE task_id = $1`,
		t.id, to); err != nil {
		return err
	}
	if to == "ready" {
		return Enqueue(ctx, tx, ev.EventID, TopicWake, map[string]any{"kind": "task_ready", "task_id": t.id, "role": t.role, "task_class": t.class})
	}
	return nil
}

// MoveTask makes one of the controller's moves (controllerMoves).
func MoveTask(ctx context.Context, tx pgx.Tx, actor, taskID, to, reason string) error {
	t, err := readTask(ctx, tx, taskID, true)
	if err != nil {
		return err
	}
	if !slices.Contains(controllerMoves[t.state], to) {
		return fmt.Errorf("%w: task %s %s -> %s is not a controller move", ErrState, t.id, t.state, to)
	}
	return moveTask(ctx, tx, actor, t, to, reason)
}

// releaseTask moves a task whose live attempt is gone (§6.2): none and branch
// back to ready, where the next claim is a new attempt (a duplicate
// computation is the accepted cost; only one attempt can be accepted);
// external to blocked, never re-dispatched by itself. t must be locked.
func releaseTask(ctx context.Context, tx pgx.Tx, actor string, t taskRow, reason string) error {
	if t.state != "leased" && t.state != "running" {
		return nil
	}
	to := "ready"
	if t.effect == "external" {
		to = "blocked"
	}
	return moveTask(ctx, tx, actor, t, to, reason)
}

// Hosts.

// RegisterHost records an Agent host and its actor (host:<id>) with one
// event. Registering the same host again with the same kind does nothing.
func RegisterHost(ctx context.Context, db DB, by, hostID, kind string) error {
	if kind != "dev" && kind != "vm" {
		return fmt.Errorf("%w: host kind %q is not dev or vm", ErrState, kind)
	}
	actor := hostActor(hostID)
	return InTx(ctx, db, func(tx pgx.Tx) error {
		var have string
		err := tx.QueryRow(ctx, `SELECT host_kind FROM agentrt.host WHERE host_id = $1`, hostID).Scan(&have)
		if err == nil {
			if have == kind {
				return nil
			}
			return fmt.Errorf("%w: host %s is registered as %s", ErrState, hostID, have)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = Append(ctx, tx, Event{Stream: "host:" + hostID, Type: "host_registered", Actor: by,
			Body: map[string]any{"host_id": hostID, "actor_id": actor, "host_kind": kind}}); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'host', $2)`,
			actor, "Agent host "+hostID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.host (host_id, actor_id, host_kind, state) VALUES ($1, $2, $3, 'active')`,
			hostID, actor, kind)
		return err
	})
}

func hostActor(hostID string) string { return "host:" + hostID }

// Controls.

type scope struct{ kind, ref string }

// held reports whether a control of one of kinds is set on any of scopes.
// stop ends running work and stops new work; pause and drain only stop new
// work (drain is for hosts).
func held(ctx context.Context, tx pgx.Tx, scopes []scope, kinds ...string) (bool, error) {
	for _, s := range scopes {
		var hit bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.control WHERE cleared_at IS NULL
		  AND control_kind = ANY($3) AND scope = $1 AND scope_ref IS NOT DISTINCT FROM NULLIF($2::text, ''))`,
			s.kind, s.ref, kinds).Scan(&hit); err != nil || hit {
			return hit, err
		}
	}
	return false, nil
}

// Claiming.

// Tool is one executor a host can run, with the model it would use.
type Tool struct {
	Executor string // actor id, e.g. exec:codex
	Provider string // model provider; required, independence is checked on it
	Model    string // may be empty until the executor starts
}

// ClaimRequest is a host's poll for work.
type ClaimRequest struct {
	HostID    string
	FreeSlots int
	Tools     []Tool // in the host's order of preference
}

// Claim is a leased attempt and what agentd needs to run it.
type Claim struct {
	TaskID, AttemptID, MissionID, Role, Repo, SpecArtifact, BaseCommit, PushedRef, Effect string
	Tool                                                                                  Tool
	AttemptNo                                                                             int
	Epoch                                                                                 int64
	HardLimit                                                                             time.Duration
	LeaseExpiresAt                                                                        time.Time
}

// dispatched are the effect classes round 2 hands out. External work is not
// dispatched in round 2 (v5 §6.2); its takeover rules are in place for when
// it is (review 148 A4).
var dispatched = []string{"none", "branch"}

type claimOpts struct {
	ttl     time.Duration
	effects []string
}

type candidate struct {
	id, mission, role, spec, subject, base, repo, effect string
	core                                                 bool
	needs                                                []string
	limitS                                               int
	created                                              time.Time
}

const candidatePage = 100

// ClaimNext leases the oldest ready task of an active mission that one of the
// host's tools may take, or returns nil. It records the poll on the host (a
// liveness column) whatever the outcome. Routing in 2.2 is v5 §5.1.2
// 「分流顺序」②–④ only: hard capability facts, the core rule and
// independence from the author; scores and quota are 2.4. Candidates are
// read in pages until one is taken or none is left, so tasks no tool here may
// take never hide the ones it may (review 148 B2).
func ClaimNext(ctx context.Context, db DB, r ClaimRequest) (*Claim, error) {
	return claimNext(ctx, db, r, claimOpts{ttl: LeaseTTL, effects: dispatched})
}

func claimNext(ctx context.Context, db DB, r ClaimRequest, o claimOpts) (*Claim, error) {
	for _, tl := range r.Tools {
		if tl.Executor == "" || tl.Provider == "" {
			return nil, errors.New("store: every tool names its executor and its model provider")
		}
	}
	var claim *Claim
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var state string
		var slots int
		err := tx.QueryRow(ctx, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() WHERE host_id = $1
		  RETURNING state, COALESCE((capacity_json->>'slots')::int, 1)`, r.HostID).Scan(&state, &slots)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: host %s is not registered", ErrFenced, r.HostID)
		}
		if err != nil || state != "active" || r.FreeSlots <= 0 || len(r.Tools) == 0 {
			return err
		}
		// A global control takes this lock exclusively (SetControl).
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(742105)`); err != nil {
			return err
		}
		var live int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.attempt WHERE host_id = $1 AND state IN ('leased','running','stale')`,
			r.HostID).Scan(&live); err != nil || live >= slots {
			return err
		}
		stop, err := held(ctx, tx, []scope{{"global", ""}, {"host", r.HostID}}, "stop", "pause", "drain")
		if err != nil || stop {
			return err
		}
		var after *candidate
		for {
			cands, err := candidates(ctx, tx, o.effects, after)
			if err != nil {
				return err
			}
			for i := range cands {
				if claim, err = tryCandidate(ctx, tx, r, cands[i], o.ttl); err != nil || claim != nil {
					return err
				}
			}
			if len(cands) < candidatePage {
				return nil
			}
			after = &cands[len(cands)-1]
		}
	})
	return claim, err
}

// candidates reads one page of ready tasks of active missions, oldest first,
// after the cursor. A review or verification of core work is core work
// (review 148 B4).
func candidates(ctx context.Context, tx pgx.Tx, effects []string, after *candidate) ([]candidate, error) {
	var at any
	var id string
	if after != nil {
		at, id = after.created, after.id
	}
	rows, err := tx.Query(ctx, `SELECT t.task_id, t.mission_id, t.role, t.spec_artifact_id,
	  COALESCE(t.subject_task_id, ''), COALESCE(t.base_commit, ''), COALESCE(t.repo, ''), t.effect_class,
	  t.core OR COALESCE(s.core, false), ARRAY(SELECT jsonb_array_elements_text(t.needs_json)), t.hard_limit_s, t.created_at
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  LEFT JOIN agentrt.task s ON s.task_id = t.subject_task_id
	  WHERE t.state = 'ready' AND m.state = 'active' AND t.effect_class = ANY($1)
	    AND ($2::timestamptz IS NULL OR (t.created_at, t.task_id) > ($2::timestamptz, $3::text))
	  ORDER BY t.created_at, t.task_id LIMIT $4`, effects, at, id, candidatePage)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanCandidate)
}

func scanCandidate(row pgx.CollectableRow) (candidate, error) {
	var c candidate
	err := row.Scan(&c.id, &c.mission, &c.role, &c.spec, &c.subject, &c.base, &c.repo, &c.effect, &c.core, &c.needs, &c.limitS, &c.created)
	return c, err
}

// tryCandidate leases c to the first tool that may take it, or returns nil.
// What it locks for a candidate it does not take is released at once by
// rolling back to a savepoint, so a skipped candidate holds nothing until the
// claim commits (review 148 B1).
func tryCandidate(ctx context.Context, tx pgx.Tx, r ClaimRequest, c candidate, ttl time.Duration) (*Claim, error) {
	tool, found, err := pickTool(ctx, tx, r.Tools, c)
	if err != nil || !found {
		return nil, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	cl, err := take(ctx, sp, r.HostID, tool, c, ttl)
	if err != nil || cl == nil {
		if rerr := sp.Rollback(ctx); err == nil {
			err = rerr
		}
		return nil, err
	}
	if err := sp.Commit(ctx); err != nil {
		return nil, err
	}
	return cl, nil
}

// take locks the candidate if no one holds it and it is still ready, then
// its mission, and leases it if the mission is still active and no control
// holds it. The mission row is shared by claims and taken exclusively by
// MoveMission and by a control on the mission, and the task row by a control
// on the task, so these checks are exact: a pause or stop either commits
// before the claim or is seen by it (review 148 A2). A plan task takes the
// mission row exclusively, so two plan tasks of one mission never lease
// together (task_one_live_plan).
func take(ctx context.Context, tx pgx.Tx, hostID string, tl Tool, c candidate, ttl time.Duration) (*Claim, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1 AND state = 'ready' FOR UPDATE SKIP LOCKED`, c.id).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	mode := "FOR SHARE"
	if c.role == "plan" {
		mode = "FOR NO KEY UPDATE"
	}
	var mission string
	if err = tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 `+mode, c.mission).Scan(&mission); err != nil {
		return nil, err
	}
	if mission != "active" {
		return nil, nil
	}
	stop, err := held(ctx, tx, []scope{{"mission", c.mission}, {"task", c.id}}, "stop", "pause")
	if err != nil || stop {
		return nil, err
	}
	if c.role == "plan" {
		var busy bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.task WHERE mission_id = $1 AND role = 'plan' AND state IN ('leased','running'))`,
			c.mission).Scan(&busy); err != nil || busy {
			return nil, err
		}
	}
	hook("taken", c.id)
	return lease(ctx, tx, hostID, tl, c, ttl)
}

func pickTool(ctx context.Context, tx pgx.Tx, tools []Tool, c candidate) (Tool, bool, error) {
	for _, tl := range tools {
		ok, err := eligible(ctx, tx, tl, c)
		if err != nil || ok {
			return tl, ok, err
		}
	}
	return Tool{}, false, nil
}

// eligible applies the hard checks to one tool. Restrictions (cannot,
// must_not, only) bind whether or not the Owner has signed them yet: a
// restriction is a tightening, and tightening needs no signature.
//   - a cannot or must_not fact on any need of the task excludes the tool;
//   - a need that some executor holds an only fact on is open to holders only;
//   - core work needs a holder of only author_core, or only review_core for a
//     review or verification; with no holder nobody may take it;
//   - a review or verification never goes to an executor or a model provider
//     that made an attempt of its subject, nor to anyone when an attempt of
//     the subject has no recorded provider (independence cannot be shown).
func eligible(ctx context.Context, tx pgx.Tx, tl Tool, c candidate) (bool, error) {
	subjects := append([]string{}, c.needs...)
	core := ""
	if c.core {
		core = "author_core"
		if c.role == "review" || c.role == "verify" {
			core = "review_core"
		}
		subjects = append(subjects, core)
	}
	var excluded bool
	err := tx.QueryRow(ctx, `SELECT
	  EXISTS (SELECT 1 FROM agentrt.capability_fact WHERE retired_at IS NULL AND fact_kind IN ('cannot','must_not')
	          AND executor_actor_id = $1 AND (model_provider IS NULL OR model_provider = $2) AND subject = ANY($3))
	  OR EXISTS (SELECT 1 FROM agentrt.capability_fact f WHERE f.retired_at IS NULL AND f.fact_kind = 'only' AND f.subject = ANY($3)
	          AND NOT EXISTS (SELECT 1 FROM agentrt.capability_fact g WHERE g.retired_at IS NULL AND g.fact_kind = 'only'
	                  AND g.subject = f.subject AND g.executor_actor_id = $1 AND (g.model_provider IS NULL OR g.model_provider = $2)))
	  OR ($4::text <> '' AND NOT EXISTS (SELECT 1 FROM agentrt.capability_fact WHERE retired_at IS NULL AND fact_kind = 'only'
	          AND subject = $4 AND executor_actor_id = $1 AND (model_provider IS NULL OR model_provider = $2)))
	  OR ($5::text <> '' AND EXISTS (SELECT 1 FROM agentrt.attempt WHERE task_id = $5
	          AND (executor_actor_id = $1 OR model_provider IS NULL OR model_provider = $2)))`,
		tl.Executor, tl.Provider, subjects, core, c.subject).Scan(&excluded)
	return !excluded, err
}

func lease(ctx context.Context, tx pgx.Tx, hostID string, tl Tool, c candidate, ttl time.Duration) (*Claim, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(attempt_no), 0) + 1 FROM agentrt.attempt WHERE task_id = $1`, c.id).Scan(&n); err != nil {
		return nil, err
	}
	id, epoch, err := leaseAttempt(ctx, tx, "controller", c.mission, c.id, tl, hostID, n, ttl)
	if err != nil {
		return nil, err
	}
	t, err := readTask(ctx, tx, c.id, true)
	if err != nil {
		return nil, err
	}
	if err = moveTask(ctx, tx, "controller", t, "leased", "claimed by host "+hostID); err != nil {
		return nil, err
	}
	cl := &Claim{TaskID: c.id, AttemptID: id, MissionID: c.mission, Role: c.role, Repo: c.repo, SpecArtifact: c.spec,
		BaseCommit: c.base, PushedRef: AttemptRef(c.id, n), Effect: c.effect, Tool: tl, AttemptNo: n, Epoch: epoch,
		HardLimit: time.Duration(c.limitS) * time.Second}
	err = tx.QueryRow(ctx, `SELECT lease_expires_at FROM agentrt.attempt WHERE attempt_id = $1`, id).Scan(&cl.LeaseExpiresAt)
	return cl, err
}

// Attempts.

// Fence names the caller of an attempt route: the host that holds the
// attempt and the epoch it was given.
type Fence struct {
	AttemptID string
	HostID    string
	Epoch     int64
}

type attemptRow struct {
	id, task, host, state        string
	no                           int
	epoch                        int64
	expires, now                 time.Time
	started, computed, persisted bool
	t                            taskRow // locked, when the caller asked for the task
}

func liveState(s string) bool { return s == "leased" || s == "running" || s == "stale" }

// lockAttempt locks the attempt row and, with withTask, then its task's row
// (attempt before task, as everywhere). The database clock is read after
// every lock is held, in a statement of its own: a clock value read before
// or during a lock wait may predate the end of the wait (T22 ⑤, review 148
// A3).
func lockAttempt(ctx context.Context, tx pgx.Tx, id string, withTask bool) (attemptRow, error) {
	var a attemptRow
	var host *string
	err := tx.QueryRow(ctx, `SELECT attempt_id, task_id, attempt_no, host_id, state, lease_epoch, lease_expires_at,
	  started_at IS NOT NULL, computed_at IS NOT NULL, persisted_at IS NOT NULL
	  FROM agentrt.attempt WHERE attempt_id = $1 FOR UPDATE`, id).
		Scan(&a.id, &a.task, &a.no, &host, &a.state, &a.epoch, &a.expires, &a.started, &a.computed, &a.persisted)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrFenced
	}
	if err != nil {
		return a, err
	}
	if host != nil {
		a.host = *host
	}
	if withTask {
		if a.t, err = readTask(ctx, tx, a.task, true); err != nil {
			return a, err
		}
	}
	err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&a.now)
	return a, err
}

// lockLive is the write-time fence of v5 §5.2 「写入时的隔离检查」 ③: with
// the attempt row (and, for routes that move the task, the task row) locked,
// the attempt's host is the caller's, its state is live, its epoch is the
// caller's, and the database time after the locks is before
// lease_expires_at plus the grace. It does not rely on the lease loop.
func lockLive(ctx context.Context, tx pgx.Tx, f Fence, withTask bool) (attemptRow, error) {
	a, err := lockAttempt(ctx, tx, f.AttemptID, withTask)
	if err != nil {
		return a, err
	}
	if a.host != f.HostID || a.epoch != f.Epoch || !liveState(a.state) || !a.now.Before(a.expires.Add(Grace)) {
		return a, ErrFenced
	}
	return a, nil
}

func attemptEvent(ctx context.Context, tx pgx.Tx, actor string, a attemptRow, typ string, body map[string]any) error {
	var mission string
	if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, a.task).Scan(&mission); err != nil {
		return err
	}
	_, err := Append(ctx, tx, Event{Stream: MissionStream(mission), Type: typ, Actor: actor, TaskID: a.task, AttemptID: a.id, Body: body})
	return err
}

// Beat is a heartbeat's answer.
type Beat struct {
	LeaseExpiresAt time.Time
	Stop           bool // a stop control covers this attempt: end it
}

// Heartbeat renews the lease. A lease that has not lapsed is extended through
// the liveness columns alone, with no event. One that has lapsed (stale, or
// past expiry before the lease loop got to it) is renewed with
// attempt_renewed and goes back to leased or running.
func Heartbeat(ctx context.Context, db DB, f Fence) (Beat, error) {
	var b Beat
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		a, err := lockLive(ctx, tx, f, false)
		if err != nil {
			return err
		}
		if a.state == "stale" || !a.now.Before(a.expires) {
			next := a.now.Add(LeaseTTL)
			to := "leased"
			if a.started {
				to = "running"
			}
			if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_renewed",
				map[string]any{"lease_expires_at": timeText(next)}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = $2, lease_expires_at = $3, heartbeat_at = clock_timestamp()
			  WHERE attempt_id = $1`, a.id, to, next); err != nil {
				return err
			}
		} else if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt
		  SET lease_expires_at = GREATEST(lease_expires_at, clock_timestamp() + make_interval(secs => $2)), heartbeat_at = clock_timestamp()
		  WHERE attempt_id = $1`, a.id, LeaseTTL.Seconds()); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT lease_expires_at FROM agentrt.attempt WHERE attempt_id = $1`, a.id).Scan(&b.LeaseExpiresAt); err != nil {
			return err
		}
		t, err := readTask(ctx, tx, a.task, false)
		if err != nil {
			return err
		}
		b.Stop, err = held(ctx, tx, []scope{{"global", ""}, {"mission", t.mission}, {"task", t.id}, {"host", f.HostID}}, "stop")
		return err
	})
	return b, err
}

// Start marks a leased attempt and its task running. The provider, when
// given, must be the one the attempt was leased for: independence was checked
// against it. Starting an attempt that already started does nothing.
func Start(ctx context.Context, db DB, f Fence, model, provider string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		a, err := lockLive(ctx, tx, f, true)
		if err != nil {
			return err
		}
		var haveModel, haveProvider string
		if err = tx.QueryRow(ctx, `SELECT COALESCE(model, ''), COALESCE(model_provider, '') FROM agentrt.attempt WHERE attempt_id = $1`,
			a.id).Scan(&haveModel, &haveProvider); err != nil {
			return err
		}
		if (provider != "" && provider != haveProvider) || (model != "" && haveModel != "" && model != haveModel) {
			return fmt.Errorf("%w: attempt %s was leased for %s %s", ErrState, a.id, haveProvider, haveModel)
		}
		if a.started {
			return nil
		}
		if a.state != "leased" {
			return fmt.Errorf("%w: attempt %s is %s", ErrState, a.id, a.state)
		}
		body := map[string]any{}
		if model != "" {
			body["model"] = model
		}
		if provider != "" {
			body["model_provider"] = provider
		}
		if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_started", body); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'running', started_at = clock_timestamp(),
		  model = COALESCE(model, NULLIF($2, '')) WHERE attempt_id = $1`, a.id, model); err != nil {
			return err
		}
		return moveTask(ctx, tx, hostActor(f.HostID), a.t, "running", "")
	})
}

// idempotent runs op at most once per key (v5 §5.2 「写入时的隔离检查」 ②).
// The key is looked up first: the same request gets the stored answer, with
// no lease check and no pre call, so a retry whose first answer was lost gets
// that answer even after the attempt stopped being live; a different request
// gets ErrConflict. Otherwise pre runs outside any transaction (a read of
// another system), then op runs in a transaction that holds a lock on the key
// and looks it up again, so concurrent retries of one request run one at a
// time and the later ones find the first one's answer. A failed op stores
// nothing.
func idempotent(ctx context.Context, db DB, actor, route, key string, request any,
	pre func(context.Context) error, op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	sum, err := canon.Sum(request)
	if err != nil {
		return nil, err
	}
	return keyed(ctx, db, actor+"|"+route+"|"+key, route, sum, pre, op)
}

func keyed(ctx context.Context, db DB, id, route, sum string,
	pre func(context.Context) error, op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	lookup := func(tx pgx.Tx) (map[string]any, bool, error) {
		var storedSum, stored string
		err := tx.QueryRow(ctx, `SELECT request_sha256, response_json::text FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
			id).Scan(&storedSum, &stored)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if storedSum != sum {
			return nil, true, ErrConflict
		}
		out, err := response([]byte(stored))
		return out, true, err
	}
	var out map[string]any
	var found bool
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, found, err = lookup(tx)
		return err
	}); err != nil || found {
		return out, err
	}
	hook("missed", id)
	if pre != nil {
		if err := pre(ctx); err != nil {
			return nil, err
		}
	}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742103, hashtext($1))`, id); err != nil {
			return err
		}
		var err error
		if out, found, err = lookup(tx); err != nil || found {
			return err
		}
		resp, err := op(tx)
		if err != nil {
			return err
		}
		rb, err := canon.Encode(resp)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.idempotency_key (idempotency_key_id, route, request_sha256, response_json, expires_at)
		  VALUES ($1, $2, $3, $4::jsonb, clock_timestamp() + interval '7 days')`, id, route, sum, string(rb)); err != nil {
			return err
		}
		out, err = response(rb)
		return err
	})
	return out, err
}

// response decodes a stored answer the same way on the first call and on a
// retry, so both return identical values.
func response(b []byte) (map[string]any, error) {
	v, err := canon.Parse(b)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("store: a stored response is not an object")
	}
	return m, nil
}

// Checkpoint records sample seq of a live attempt: the artifacts of the
// sample (log tail, patch), or that the patch was too large to keep. The key
// is the attempt plus the sample number, so each sample is recorded once and
// a retried sample gets the first answer (review 142 R2).
func Checkpoint(ctx context.Context, db DB, f Fence, seq int, artifacts []string, tooLarge bool) (map[string]any, error) {
	if seq < 1 {
		return nil, fmt.Errorf("%w: checkpoint numbers start at 1", ErrState)
	}
	req := map[string]any{"epoch": f.Epoch, "seq": seq, "artifacts": nonNil(artifacts), "too_large": tooLarge}
	return idempotent(ctx, db, hostActor(f.HostID), "checkpoint", fmt.Sprintf("%s:%d", f.AttemptID, seq), req, nil,
		func(tx pgx.Tx) (map[string]any, error) {
			a, err := lockLive(ctx, tx, f, false)
			if err != nil {
				return nil, err
			}
			if err = haveArtifacts(ctx, tx, artifacts); err != nil {
				return nil, err
			}
			body := map[string]any{"checkpoint_seq": seq}
			if len(artifacts) > 0 {
				body["artifacts"] = artifacts
			}
			if tooLarge {
				body["too_large"] = true
			}
			if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_checkpointed", body); err != nil {
				return nil, err
			}
			return map[string]any{"checkpoint_seq": seq}, nil
		})
}

var endHows = []string{"exit", "hard_limit", "over_capacity", "cancelled", "daemon_restart", "host_reboot", "lease_lost"}

// Computed records that the executor ended and its result is on the host.
// exitCode is nil when the process had none (killed by a signal).
func Computed(ctx context.Context, db DB, f Fence, exitCode *int, endHow, conclusion string) (map[string]any, error) {
	if !slices.Contains(endHows, endHow) || !slices.Contains([]string{"done", "failed", "none"}, conclusion) {
		return nil, fmt.Errorf("%w: end %q and conclusion %q", ErrState, endHow, conclusion)
	}
	req := map[string]any{"epoch": f.Epoch, "end_how": endHow, "conclusion": conclusion}
	body := map[string]any{"end_how": endHow, "conclusion": conclusion}
	if exitCode != nil {
		req["exit_code"], body["exit_code"] = *exitCode, *exitCode
	}
	return idempotent(ctx, db, hostActor(f.HostID), "computed", f.AttemptID, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		a, err := lockLive(ctx, tx, f, false)
		if err != nil {
			return nil, err
		}
		if !a.started || a.computed {
			return nil, fmt.Errorf("%w: attempt %s is not started-and-uncomputed", ErrState, a.id)
		}
		if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_computed", body); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET computed_at = clock_timestamp(), exit_code = $2, end_how = $3, conclusion = $4
		  WHERE attempt_id = $1`, a.id, exitCode, endHow, conclusion); err != nil {
			return nil, err
		}
		return map[string]any{"computed": true}, nil
	})
}

// BranchHead reads the commit a branch of the main repository points at. The
// HTTP layer passes a Gitea reader, tests a fake.
type BranchHead func(ctx context.Context, ref string) (string, error)

// Result is what agentd hands over: the report, other artifacts, and the
// branch and commit it pushed, if it pushed one.
type Result struct {
	Report    string
	Artifacts []string
	Ref       string
	Commit    string
}

func (r Result) request(epoch int64) map[string]any {
	return map[string]any{"epoch": epoch, "report": r.Report, "artifacts": nonNil(r.Artifacts), "ref": r.Ref, "commit": r.Commit}
}

func (r Result) all() []string {
	if r.Report == "" {
		return r.Artifacts
	}
	return append([]string{r.Report}, r.Artifacts...)
}

// checkHead is the Gitea half of the result check: the branch points at the
// commit reported.
func (r Result) checkHead(head BranchHead) func(context.Context) error {
	return func(ctx context.Context) error {
		if (r.Ref == "") != (r.Commit == "") {
			return fmt.Errorf("%w: a pushed branch needs both its ref and its commit", ErrState)
		}
		if r.Ref == "" {
			return nil
		}
		got, err := head(ctx, r.Ref)
		if err != nil {
			return fmt.Errorf("store: read %s: %w", r.Ref, err)
		}
		if got != r.Commit {
			return fmt.Errorf("%w: %s is at %s, not %s", ErrState, r.Ref, got, r.Commit)
		}
		return nil
	}
}

// fits checks the result against the attempt and the database: the branch is
// the attempt's own, a branch task pushed one and a none task did not, and
// every artifact is stored here.
func (r Result) fits(ctx context.Context, tx pgx.Tx, a attemptRow) error {
	var effect string
	if err := tx.QueryRow(ctx, `SELECT effect_class FROM agentrt.task WHERE task_id = $1`, a.task).Scan(&effect); err != nil {
		return err
	}
	switch {
	case r.Ref != "" && r.Ref != AttemptRef(a.task, a.no):
		return fmt.Errorf("%w: attempt %s pushes only to %s", ErrState, a.id, AttemptRef(a.task, a.no))
	case effect == "branch" && r.Ref == "":
		return fmt.Errorf("%w: a branch task's result is a pushed branch", ErrState)
	case effect == "none" && r.Ref != "":
		return fmt.Errorf("%w: a task of effect none pushes nothing", ErrState)
	}
	return haveArtifacts(ctx, tx, r.all())
}

func (r Result) body() map[string]any {
	body := map[string]any{}
	if all := r.all(); len(all) > 0 {
		body["artifacts"] = all
	}
	if r.Ref != "" {
		body["pushed_ref"], body["pushed_commit"] = r.Ref, r.Commit
	}
	return body
}

// Persist records that the result has left the host (attempt_persisted):
// the report and artifacts are stored here, and the branch, if any, points at
// the commit reported. Only a persisted attempt can be accepted.
func Persist(ctx context.Context, db DB, f Fence, r Result, head BranchHead) (map[string]any, error) {
	if r.Report == "" {
		return nil, fmt.Errorf("%w: a result has a report", ErrState)
	}
	return idempotent(ctx, db, hostActor(f.HostID), "persist", f.AttemptID, r.request(f.Epoch), r.checkHead(head),
		func(tx pgx.Tx) (map[string]any, error) {
			a, err := lockLive(ctx, tx, f, false)
			if err != nil {
				return nil, err
			}
			if !a.computed || a.persisted {
				return nil, fmt.Errorf("%w: attempt %s is not computed-and-unpersisted", ErrState, a.id)
			}
			if err = r.fits(ctx, tx, a); err != nil {
				return nil, err
			}
			if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_persisted", r.body()); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET persisted_at = clock_timestamp(), report_artifact_id = $2,
			  pushed_ref = NULLIF($3, ''), pushed_commit = NULLIF($4, '') WHERE attempt_id = $1`, a.id, r.Report, r.Ref, r.Commit); err != nil {
				return nil, err
			}
			return map[string]any{"persisted": true}, nil
		})
}

// Submit hands a persisted attempt over for verification. The key is the
// attempt: an attempt has one result, so this is the same as keying by its
// pushed commit.
func Submit(ctx context.Context, db DB, f Fence) (map[string]any, error) {
	return idempotent(ctx, db, hostActor(f.HostID), "submit", f.AttemptID, map[string]any{"epoch": f.Epoch}, nil,
		func(tx pgx.Tx) (map[string]any, error) {
			a, err := lockLive(ctx, tx, f, true)
			if err != nil {
				return nil, err
			}
			if !a.persisted {
				return nil, fmt.Errorf("%w: attempt %s is not persisted", ErrState, a.id)
			}
			if err = attemptEvent(ctx, tx, hostActor(f.HostID), a, "attempt_submitted", map[string]any{}); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'submitted' WHERE attempt_id = $1`, a.id); err != nil {
				return nil, err
			}
			if err = moveTask(ctx, tx, hostActor(f.HostID), a.t, "submitted", ""); err != nil {
				return nil, err
			}
			return map[string]any{"submitted": true}, nil
		})
}

// Salvage keeps the result of an attempt that lost its lease (v5 §5.2,
// §6.2). Only the attempt's host may salvage it, and only once its lease plus
// the grace has passed, whatever state the attempt is in (review 148 A1):
// before that the answer is ErrInGrace and agentd tries again later.
// Attempts that were submitted have nothing to salvage. The result is checked
// as persist checks it; when it holds a report and the attempt was not
// persisted, the attempt gets persisted_at, so a new verification task may
// accept it. Neither the attempt's state nor the task's changes.
func Salvage(ctx context.Context, db DB, attemptID, hostID string, r Result, head BranchHead) (map[string]any, error) {
	return idempotent(ctx, db, hostActor(hostID), "salvage", attemptID, r.request(0), r.checkHead(head),
		func(tx pgx.Tx) (map[string]any, error) {
			a, err := lockAttempt(ctx, tx, attemptID, false)
			if err != nil {
				return nil, err
			}
			if a.host != hostID {
				return nil, ErrFenced
			}
			if a.state == "submitted" || a.state == "finished" {
				return nil, fmt.Errorf("%w: attempt %s was %s", ErrState, a.id, a.state)
			}
			if a.now.Before(a.expires.Add(Grace)) {
				return nil, ErrInGrace
			}
			if err = r.fits(ctx, tx, a); err != nil {
				return nil, err
			}
			if err = attemptEvent(ctx, tx, hostActor(hostID), a, "attempt_salvaged", r.body()); err != nil {
				return nil, err
			}
			persisted := a.persisted
			if !persisted && r.Report != "" {
				if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET persisted_at = clock_timestamp(), report_artifact_id = $2,
				  pushed_ref = NULLIF($3, ''), pushed_commit = NULLIF($4, '') WHERE attempt_id = $1`, a.id, r.Report, r.Ref, r.Commit); err != nil {
					return nil, err
				}
				persisted = true
			}
			return map[string]any{"salvaged": true, "persisted": persisted}, nil
		})
}

// Ended records that the attempt's process group is gone on its host
// (attempt_ended). An attempt already ended keeps its state; a live one ends
// as failed and its task moves by effect class (§6.2, T01). The key is the
// attempt, so a repeated report gets the first answer.
func Ended(ctx context.Context, db DB, attemptID, hostID, endHow string, groupCleared bool) (map[string]any, error) {
	if !slices.Contains(endHows, endHow) {
		return nil, fmt.Errorf("%w: end %q", ErrState, endHow)
	}
	req := map[string]any{"end_how": endHow, "group_cleared": groupCleared}
	return idempotent(ctx, db, hostActor(hostID), "ended", attemptID, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		a, err := lockAttempt(ctx, tx, attemptID, true)
		if err != nil {
			return nil, err
		}
		if a.host != hostID {
			return nil, ErrFenced
		}
		if err = attemptEvent(ctx, tx, hostActor(hostID), a, "attempt_ended",
			map[string]any{"end_how": endHow, "group_cleared": groupCleared}); err != nil {
			return nil, err
		}
		if !liveState(a.state) {
			return map[string]any{"state": a.state}, nil
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'failed', ended_at = clock_timestamp(),
		  end_how = COALESCE(end_how, $2) WHERE attempt_id = $1`, a.id, endHow); err != nil {
			return nil, err
		}
		if err = releaseTask(ctx, tx, hostActor(hostID), a.t, "attempt ended on its host: "+endHow); err != nil {
			return nil, err
		}
		return map[string]any{"state": "failed"}, nil
	})
}

// SweepLeases is one pass of the lease loop (v5 §5.2 控制器「租约」): live
// attempts past their expiry become stale; past the grace they become lost
// and their task moves by effect class. It reads only attempts that need one
// of these moves, so stale attempts within their grace never crowd out the
// rest (review 148 B3). It only changes state; the write-time fence is what
// stops late writes. It returns how many attempts it moved.
func SweepLeases(ctx context.Context, db DB) (int, int, error) {
	var stale, lost int
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT attempt_id FROM agentrt.attempt
		  WHERE (state IN ('leased','running') AND lease_expires_at < clock_timestamp())
		     OR (state = 'stale' AND lease_expires_at + make_interval(secs => $1) <= clock_timestamp())
		  ORDER BY lease_expires_at LIMIT 100 FOR UPDATE SKIP LOCKED`, Grace.Seconds())
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			a, err := lockAttempt(ctx, tx, id, true)
			if err != nil {
				return err
			}
			switch {
			case !a.now.Before(a.expires.Add(Grace)):
				if err = attemptEvent(ctx, tx, "controller", a, "attempt_lost",
					map[string]any{"reason": "lease expired at " + timeText(a.expires) + " and the grace passed"}); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'lost', ended_at = clock_timestamp(),
				  end_how = COALESCE(end_how, 'lease_lost') WHERE attempt_id = $1`, a.id); err != nil {
					return err
				}
				if err = releaseTask(ctx, tx, "controller", a.t, "attempt lost"); err != nil {
					return err
				}
				lost++
			case a.state != "stale":
				if err = attemptEvent(ctx, tx, "controller", a, "attempt_stale",
					map[string]any{"lease_expires_at": timeText(a.expires)}); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET state = 'stale' WHERE attempt_id = $1`, a.id); err != nil {
					return err
				}
				stale++
			}
		}
		return nil
	}); err != nil {
		return 0, 0, err
	}
	return stale, lost, nil
}

func haveArtifacts(ctx context.Context, tx pgx.Tx, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var missing []string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id ORDER BY id), '{}') FROM unnest($1::text[]) AS id
	  WHERE NOT EXISTS (SELECT 1 FROM agentrt.artifact WHERE artifact_id = id)`, ids).Scan(&missing); err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: artifacts not stored: %v", ErrState, missing)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
