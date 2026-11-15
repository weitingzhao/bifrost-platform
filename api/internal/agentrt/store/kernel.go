package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
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
// it within Grace; after that it is lost. A claim and a heartbeat take the
// tuning rule's lease_ttl_s instead, when one is in force (leaseTTL).
const (
	LeaseTTL = 120 * time.Second
	Grace    = 15 * time.Minute
)

var (
	// ErrFenced: the caller does not hold this attempt (another host, an old
	// epoch, an attempt that is no longer live, or past expiry plus grace).
	ErrFenced = errors.New("store: this attempt's lease is not yours")
	// ErrConflict: an idempotency key reused with a different request.
	ErrConflict = actor.ErrConflict
	// ErrInGrace: salvage refused because the lease may still be renewed.
	ErrInGrace = errors.New("store: the attempt is still within its grace period")
	// ErrState: the operation does not fit the current state.
	ErrState = eventlog.ErrState
)

// DB is what the kernel needs: a pool or a connection (eventlog.DB).
type DB = eventlog.DB

// testHook, set only by tests, runs at named points so a test can hold a
// call there and observe or race it.
// It is loaded atomically: a test may remove it while a call it staged on
// another goroutine still passes a hook point.
var testHook = &actor.TestHook

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

// Hosts.

// hostKinds are the kinds of host (0010): an Agent host of the cluster's
// (dev, vm), or the one external host, the coordinator's Mac, which leases
// only the tasks pinned to it (DESIGN-coordinator-landing-v3 §2.4); the
// database holds an external host to those and allows one.
var hostKinds = []string{"dev", "vm", HostExternal}

// HostExternal is the kind of the external host.
const HostExternal = "external"

// Controls.
