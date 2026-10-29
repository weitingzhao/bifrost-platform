package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// The kernel: the state machine of missions, tasks and attempts that the
// HTTP routes of design v5 §5.2 call. Every change is an event plus the row
// it names, in one transaction; missions, tasks and steps move only along
// their transition tables (T25).
//
// Lock order, everywhere: the writer of a route (its own lock and, when this
// write registers it, the registration lock and the system stream; review
// 176 A1), then the dependency graph, then steps, then attempts, then tasks
// (verification, Conclude, takes a subject before its check), then mission,
// then the event stream (Append): every write takes the row locks it needs
// before it appends (review 149 B1). A batch of proposals never waits while
// it holds a task lock: it takes steps and subjects NOWAIT and the tasks it
// depends on FOR KEY SHARE, which no state change conflicts with (review 176
// A2).
// Claims start at the host row and the shared global and capability locks,
// then the task, the mission row, and never lock an existing attempt; a
// candidate a claim does not take is released at once. A write of job
// candidates or capability facts takes the capability lock exclusively
// (LockCapabilities) before any row lock and before the stream it appends to
// (a rule write appends to its own rule:<id>); only the writer's lock and a
// new thread's registration (lock 742109, the system stream) come before it,
// and nothing that holds it waits for those (reviews 187 R1, 188 R1).
// Task rows are locked FOR NO KEY UPDATE, which foreign key checks (KEY
// SHARE) never wait for.

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
// It is loaded atomically: a test may remove it while a call it staged on
// another goroutine still passes a hook point.
var testHook atomic.Pointer[func(point, arg string)]

func hook(point, arg string) {
	if f := testHook.Load(); f != nil {
		(*f)(point, arg)
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
	return moveMission(ctx, tx, actor, missionID, to, reason, "", "")
}

// moveMission is MoveMission naming the decision card the move is about and,
// when the move acts on its answer, the option answered.
func moveMission(ctx context.Context, tx pgx.Tx, actor, missionID, to, reason, question, answer string) error {
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
	if question != "" {
		body["question_id"] = question
	}
	if answer != "" {
		body["answer"] = answer
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
	"submitted":      {"verifying", "accepted", "blocked"}, // accepted: a verification or review task once its result is used
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
		q += ` FOR NO KEY UPDATE`
	}
	var t taskRow
	err := tx.QueryRow(ctx, q, id).Scan(&t.id, &t.mission, &t.step, &t.state, &t.effect, &t.role, &t.class)
	return t, err
}

// moveTask writes task_state_changed and the new state. t must be locked. A
// task that becomes ready gets its route (route_decided) and wakes the hosts.
func moveTask(ctx context.Context, tx pgx.Tx, actor string, t taskRow, to, reason string) error {
	return moveTaskBy(ctx, tx, actor, t, to, reason, taskMoves)
}

// failWork blocks a task whose work did not pass, its checks failed with no
// round left or nothing usable handed in: the in-job scores count it as a
// failure of the executor of its last attempt (scores.go). A task blocked for
// any other reason (waiting, a mission that ended) counts for nobody.
func failWork(ctx context.Context, tx pgx.Tx, actor string, t taskRow, reason string) error {
	return moveTaskWith(ctx, tx, actor, t, "blocked", reason, taskMoves, true)
}

// moveTaskBy moves a task by the given table of moves (a path with moves of
// its own, as accepting a salvaged result, keeps them out of taskMoves).
func moveTaskBy(ctx context.Context, tx pgx.Tx, actor string, t taskRow, to, reason string, moves map[string][]string) error {
	return moveTaskWith(ctx, tx, actor, t, to, reason, moves, false)
}

func moveTaskWith(ctx context.Context, tx pgx.Tx, actor string, t taskRow, to, reason string, moves map[string][]string, failed bool) error {
	if !slices.Contains(moves[t.state], to) {
		return fmt.Errorf("%w: task %s %s -> %s", ErrState, t.id, t.state, to)
	}
	body := map[string]any{"from": t.state, "to": to}
	if reason != "" {
		body["reason"] = reason
	}
	if failed {
		body["failed"] = true
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
		if err := decideRoute(ctx, tx, t); err != nil {
			return err
		}
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
	return registerHost(ctx, db, by, hostID, kind, "", false)
}

// RegisterAgentdHost registers, on its first claim, the host an agentd token
// is bound to (Owner 2026-10-14 「主机登记 A」: adding the token, bound to the
// host, is the authorization). A host registered already, in any state, is
// left as it is; the event names the token. A host a standing envelope in
// force names as an external host is refused (ErrExternalHost): only its
// exact claim registers it, as the external host (pinclaim.go).
func RegisterAgentdHost(ctx context.Context, db DB, hostID, token string) error {
	return registerHost(ctx, db, "controller", hostID, "dev", token, true)
}

func registerHost(ctx context.Context, db DB, by, hostID, kind, token string, anyKind bool) error {
	if !slices.Contains(hostKinds, kind) {
		return fmt.Errorf("%w: host kind %q is not one of %v", ErrState, kind, hostKinds)
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		return registerHostIn(ctx, tx, by, hostID, kind, token, anyKind)
	})
}

// registerHostIn keeps registration and its caller's authorization in one transaction.
func registerHostIn(ctx context.Context, tx pgx.Tx, by, hostID, kind, token string, anyKind bool) error {
	actor := hostActor(hostID)
	// One registration of a host at a time: a second first claim waits,
	// then finds it registered (review 190 B5).
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742112, hashtext($1))`, hostID); err != nil {
		return err
	}
	var have string
	err := tx.QueryRow(ctx, `SELECT host_kind FROM agentrt.host WHERE host_id = $1`, hostID).Scan(&have)
	if err == nil {
		if have == kind || anyKind {
			return nil
		}
		return fmt.Errorf("%w: host %s is registered as %s", ErrState, hostID, have)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	// A host a standing envelope names as the external host is
	// registered only as that, by its first exact claim (pinclaim.go):
	// an agentd call that would make it an ordinary host is refused
	// (DESIGN-coordinator-landing-v3 §2.4; the coordinating thread,
	// 2026-10-29, the strictest reading).
	if kind != HostExternal {
		named, err := externalNamed(ctx, tx, hostID)
		if err != nil {
			return err
		}
		if named {
			return fmt.Errorf("%w: host %s is the external host a standing envelope names; only its exact claim registers it",
				ErrExternalHost, hostID)
		}
	}
	body := map[string]any{"host_id": hostID, "actor_id": actor, "host_kind": kind}
	if token != "" {
		body["token"] = token
	}
	if _, err = Append(ctx, tx, Event{Stream: "host:" + hostID, Type: "host_registered", Actor: by, Body: body}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'host', $2)`,
		actor, "Agent host "+hostID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.host (host_id, actor_id, host_kind, state) VALUES ($1, $2, $3, 'active')`,
		hostID, actor, kind)
	return err
}

func hostActor(hostID string) string { return "host:" + hostID }

// hostKinds are the kinds of host (0010): an Agent host of the cluster's
// (dev, vm), or the one external host, the coordinator's Mac, which leases
// only the tasks pinned to it (DESIGN-coordinator-landing-v3 §2.4); the
// database holds an external host to those and allows one.
var hostKinds = []string{"dev", "vm", HostExternal}

// HostExternal is the kind of the external host.
const HostExternal = "external"

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
