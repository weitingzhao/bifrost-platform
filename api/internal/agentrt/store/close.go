package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Closing a goal's mission (W-31 2.2b, iteration 1: a goal that was done
// still showed as active). A mission a goal made is bound to no plan step;
// once every one of its tasks is accepted, or was dropped by the Owner on its
// blocked card (blockedcard.go: cancelled in the transaction that records
// owner_wait_closed with outcome dropped), nothing is left for it to do, and
// the controller moves it on (mission_state_changed): to done when an
// implementation (or, for a message to the chief of staff, its brief) is
// among the accepted, to stopped when the Owner dropped the rest and nothing
// the goal asked for was accepted (its planning, say). A mission bound to plan
// steps is left to its steps and their rules. One with a task that is neither
// (open, rejected, blocked, or cancelled any other way) stays as it is, for
// the Owner or the coordinator to see why.
//
// A check the controller cancelled because its subject was stopped
// (stoppedchecks.go: its reason begins with stoppedSubject) does not hold its
// mission open either: it could never have decided anything. Nor does a task
// its proposer withdrew (withdrawnTask, as the progress view reads it), one
// cancelled or rejected after its change reached main (a 'landed' note), or
// one cancelled while a stop held it, its own, its subject's or its
// mission's, or that a stop holds now (PROD 2026-11-06: missions whose tasks
// were all finished stayed active until the coordinator stopped them by
// hand). An implementation with a 'landed' note, whatever its state, counts
// as delivered, as an accepted one does. A task cancelled any other way (on a
// mission closed on a card, by a plan change) holds its mission open until
// someone decides what it means.
//
// A task proposed over HTTP while the mission closes may land in a done
// mission (the proposal reads the mission's state without its lock); it stays
// proposed there, since dispatch readies tasks of active missions only. A
// goal's tasks come from its plan, in the controller's own pass, so this does
// not arise for them.

// ownerDropped is a task (t) the Owner dropped on its card.
const ownerDropped = `t.state = 'cancelled' AND EXISTS (SELECT 1 FROM agentrt.event d WHERE d.task_id = t.task_id
    AND d.event_type = 'owner_wait_closed' AND d.body_json->>'outcome' = 'dropped')`

// checkClosed is a check (t) CloseStoppedChecks cancelled: its last move's
// reason begins with stoppedSubject (stoppedchecks.go).
const checkClosed = `t.state = 'cancelled' AND t.role IN ('verify','review') AND EXISTS (SELECT 1 FROM (SELECT e.body_json FROM agentrt.event e
    WHERE e.task_id = t.task_id AND e.event_type = 'task_state_changed' ORDER BY e.event_id DESC LIMIT 1) l
  WHERE starts_with(l.body_json->>'reason', '` + stoppedSubject + `'))`

// landedNoteExists is the condition that task t has a note saying its change is
// on main (prune.go records it).
const landedNoteExists = `EXISTS (SELECT 1 FROM agentrt.event n WHERE n.task_id = t.task_id AND n.event_type = 'note' AND n.body_json ? 'landed')`

// onMain is a task (t) cancelled or rejected after its change reached main.
const onMain = `t.state IN ('cancelled','rejected') AND ` + landedNoteExists

// stoppedEmpty is a cancelled task (t) that a stop control (its own, its
// subject's or its mission's, as stopHolding reads it) held when it was
// cancelled, or holds now.
const stoppedEmpty = `t.state = 'cancelled' AND EXISTS (SELECT 1 FROM agentrt.control c WHERE c.control_kind = 'stop' AND ` + stopScope + `
    AND (c.cleared_at IS NULL OR EXISTS (SELECT 1 FROM (SELECT max(x.at) AS at FROM agentrt.event x WHERE x.task_id = t.task_id
      AND (x.event_type = 'task_cancelled' OR x.event_type = 'task_state_changed' AND x.body_json->>'to' = 'cancelled')) k
      WHERE c.set_at <= k.at AND c.cleared_at > k.at)))`

// supersededTask recognizes the existing Owner-approved cancellation format.
// Recheck the replacement instead of treating arbitrary cancellation as delivery.
const supersededTask = `t.state = 'cancelled' AND EXISTS (SELECT 1 FROM ` + lastCancelEvent + `
  JOIN agentrt.task replacement ON replacement.task_id = split_part(e.body_json->>'reason', ' ', 3)
  WHERE e.actor_id = 'owner' AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'cancelled'
    AND e.body_json->>'reason' ~ '^superseded by t_[A-Z0-9]+ at [a-f0-9]{40}; approval appr_[a-f0-9]{16}: '
    AND replacement.mission_id = t.mission_id AND replacement.repo IS NOT DISTINCT FROM t.repo
    AND replacement.role = 'implement' AND replacement.state = 'accepted'
    AND EXISTS (SELECT 1 FROM agentrt.event proof WHERE proof.task_id = replacement.task_id
      AND proof.event_type = 'note' AND proof.body_json->>'landed' = rtrim(split_part(e.body_json->>'reason', ' ', 5), ';')))`

// dropped is a task (t) that no longer holds its mission open though it was
// not accepted: the Owner dropped it, it was a check of a stopped task, its
// proposer withdrew it, its change is on main, or a stop left nothing for it.
const dropped = `((` + ownerDropped + `) OR (` + checkClosed + `) OR (` + withdrawnTask + `) OR (` + onMain + `) OR (` + stoppedEmpty + `) OR (` + supersededTask + `))`

// delivered is the condition that mission m has what its goal asked for: an
// implementation (or brief) accepted, or an implementation on main.
const delivered = `EXISTS (SELECT 1 FROM agentrt.task t WHERE t.mission_id = m.mission_id
    AND ((t.role IN ('implement','brief') AND t.state = 'accepted') OR (t.role = 'implement' AND ` + landedNoteExists + `)))`

// closable is the condition, read again under the mission's lock.
const closable = `m.state = 'active'
  AND NOT EXISTS (SELECT 1 FROM agentrt.plan_node n WHERE n.mission_id = m.mission_id)
  AND (` + delivered + ` OR EXISTS (SELECT 1 FROM agentrt.task t WHERE t.mission_id = m.mission_id AND ` + dropped + `))
  AND NOT EXISTS (SELECT 1 FROM agentrt.task t WHERE t.mission_id = m.mission_id AND t.state <> 'accepted' AND NOT (` + dropped + `))`

// CloseMissions is one pass: it closes every mission done as above and
// returns their ids.
func CloseMissions(ctx context.Context, db DB) ([]string, error) {
	var ids []string
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT m.mission_id FROM agentrt.mission m WHERE `+closable+` ORDER BY m.mission_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	var closed []string
	var holdErrs []error
	folds := landingFolds{}
	for _, id := range ids {
		did := false
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			var accepted, drops, checks, withdrawals, landed, stops int
			var done bool
			// Each task is counted once, under the first reason that applies.
			count := func(cond, before string) string {
				return `(SELECT count(*) FROM agentrt.task t WHERE t.mission_id = m.mission_id AND (` + cond + `) AND NOT ((` + before + `)))`
			}
			err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.task WHERE mission_id = m.mission_id AND state = 'accepted'),
			    (SELECT count(*) FROM agentrt.task t WHERE t.mission_id = m.mission_id AND `+ownerDropped+`),
			    `+count(checkClosed, ownerDropped)+`,
			    `+count(withdrawnTask, ownerDropped+`) OR (`+checkClosed)+`,
			    `+count(onMain, ownerDropped+`) OR (`+checkClosed+`) OR (`+withdrawnTask)+`,
			    `+count(stoppedEmpty, ownerDropped+`) OR (`+checkClosed+`) OR (`+withdrawnTask+`) OR (`+onMain)+`,
			    `+delivered+`
			  FROM agentrt.mission m WHERE m.mission_id = $1 AND `+closable+` FOR NO KEY UPDATE OF m`, id).Scan(&accepted, &drops, &checks, &withdrawals, &landed, &stops, &done)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			// A savepoint: an unreadable landing stream holds this mission
			// only, and the pass goes on with the others.
			sp, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			// A mission with a root in the live landing scope closes from
			// landing evidence (B4, CUT §11.8 v3); any other by the hold
			// below, as before.
			b4, held, to, why, err := landingCloseB4(ctx, sp, folds, id)
			if err == nil && !b4 {
				held, err = landingHolds(ctx, sp, folds, id, eventClock())
			}
			if err != nil {
				holdErrs = append(holdErrs, fmt.Errorf("mission %s: landing: %w", id, err))
				if rerr := sp.Rollback(ctx); rerr != nil {
					return rerr
				}
				return nil
			}
			if err = sp.Commit(ctx); err != nil || held {
				return err
			}
			did = true
			if b4 {
				return MoveMission(ctx, tx, "controller", id, to, why)
			}
			// Closed checks, withdrawn tasks, tasks on main and stopped ones
			// are named only when there are some, so a mission with none says
			// what it said before.
			orChecks, andChecks := "", ""
			if checks > 0 {
				orChecks = fmt.Sprintf(" or was a check of a stopped task, closed by the controller (%d)", checks)
				andChecks = fmt.Sprintf(", and checks of stopped tasks were closed by the controller (%d)", checks)
			}
			var others []string
			for _, o := range []struct {
				n    int
				text string
			}{{withdrawals, "withdrawn by its proposer"}, {landed, "on main"}, {stops, "stopped with nothing left"}} {
				if o.n > 0 {
					others = append(others, fmt.Sprintf("%s (%d)", o.text, o.n))
				}
			}
			more := ""
			if len(others) > 0 {
				more = ", " + strings.Join(others, ", ")
			}
			const nothing = "; nothing the goal asked for was accepted"
			switch {
			case drops == 0 && done:
				return MoveMission(ctx, tx, "controller", id, "done", fmt.Sprintf("every task is accepted (%d)%s%s", accepted, orChecks, more))
			case done:
				return MoveMission(ctx, tx, "controller", id, "done",
					fmt.Sprintf("every task is accepted (%d) or was dropped by the Owner on its card (%d)%s%s", accepted, drops, orChecks, more))
			case drops == 0 && checks > 0:
				return MoveMission(ctx, tx, "controller", id, "stopped",
					fmt.Sprintf("its remaining tasks were checks of stopped tasks, closed by the controller (%d)%s%s", checks, more, nothing))
			case drops == 0: // closable: then some task was withdrawn, on main or stopped
				return MoveMission(ctx, tx, "controller", id, "stopped",
					fmt.Sprintf("its remaining tasks were %s%s", strings.Join(others, ", "), nothing))
			}
			return MoveMission(ctx, tx, "controller", id, "stopped",
				fmt.Sprintf("the Owner dropped its remaining tasks on their cards (%d)%s%s%s", drops, andChecks, more, nothing))
		}); err != nil {
			return closed, errors.Join(append(holdErrs, fmt.Errorf("mission %s: %w", id, err))...)
		}
		if did {
			closed = append(closed, id)
		}
	}
	return closed, errors.Join(holdErrs...)
}

// landingHolds: an implementation the landing loop is carrying to main keeps
// its mission active (CUT-landing §2.7's hold, brought forward for the S2
// drill). Every landing gate reads the mission as active, so closing it would
// end that operation stale before its CI finished. It holds while
//   - the root is pending (landing.RootPending: its latest operation has not
//     ended, or ended discarded, non_ff or retried, so k+1 is due), and at
//     most landingHoldLimit after the root's first claim (or retry) — the
//     six-hour bound plus a margin, so a mission is released even when no
//     loop runs to record failed{bound}; an operation at main
//     (landing.AtMain) holds without that limit until it is reconciled;
//   - or the root is in the first-claim handoff (TD-447 4, CUT §7.7) and has
//     no operation yet. The handoff is decided by durable facts only: the
//     repository's stream recorded landing enabled, and the mission was
//     created and the task accepted no earlier than that, and the mission is
//     not no_merge. The current rule plays no part: a rule withdrawn or
//     expired pauses claims, it does not drop a root (§5 item 5). The root
//     stays held until the stream has its first operation: a claim, or the
//     claim's record that it never will be (failed{stale|result|in_main|
//     reverted|bound}, born terminal as land:<root>:1). A claim whose Git
//     reads outlast any timer no longer loses to a close.
//
// A handoff root still without an operation landing.ClaimWindow after its
// acceptance is ended here, in this transaction, with failed{bound} before
// the mission is let go: a mission never closes over a root with no landing
// record, whether the loop ran or not. Without a recorded enable (PROD today)
// no never-claimed root is held. The caller holds the mission's lock;
// ClaimLandingDrill and ApplyLandingAnswer lock the task, then its mission,
// before the repository stream, and the stream is always the last lock, so a
// claim and a close are ordered and the append here closes no cycle.
const landingHoldLimit = landing.ClaimWindow + 15*time.Minute

func landingHolds(ctx context.Context, tx pgx.Tx, folds landingFolds, mission string, now time.Time) (bool, error) {
	return landingHoldsExcept(ctx, tx, folds, mission, now, nil)
}

func landingHoldsExcept(ctx context.Context, tx pgx.Tx, folds landingFolds, mission string, now time.Time, except map[string]bool) (bool, error) {
	type root struct {
		task     string
		accepted *time.Time
	}
	rows, err := tx.Query(ctx, `SELECT t.task_id, t.repo, (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id = t.task_id
	    AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'accepted')
	  FROM agentrt.task t WHERE t.mission_id = $1 AND t.role = 'implement' AND t.repo <> '' ORDER BY t.repo, t.task_id`, mission)
	if err != nil {
		return false, err
	}
	roots, repos := map[string][]root{}, []string{}
	for rows.Next() {
		var r root
		var repo string
		if err = rows.Scan(&r.task, &repo, &r.accepted); err != nil {
			rows.Close()
			return false, err
		}
		if except[repo] {
			continue
		}
		if len(roots[repo]) == 0 {
			repos = append(repos, repo)
		}
		roots[repo] = append(roots[repo], r)
	}
	rows.Close()
	if err = rows.Err(); err != nil || len(repos) == 0 {
		return false, err
	}
	var created time.Time
	var noMerge bool
	if err = tx.QueryRow(ctx, `SELECT created_at, COALESCE(scope_json->>'no_merge','') <> '' FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&created, &noMerge); err != nil {
		return false, err
	}
	slices.Sort(repos) // byte order, independent of database collation
	held := false
	for _, repo := range repos {
		stream := "landing:" + repo
		state, err := folds.state(ctx, tx, stream)
		if err != nil {
			return false, err
		}
		// A no_merge mission is never claimed (the gate reads it stale).
		handoff := !noMerge && !state.Fence().IsZero() && !state.EnabledAt.IsZero() && !created.Before(state.Fence())
		for _, r := range roots[repo] {
			if latest, ok := state.Operations[state.LatestOperation(r.task)]; ok {
				if !state.RootPending(r.task) {
					continue
				}
				// At main the outcome is unknown until reconciled with main:
				// no time limit releases the mission (TD-447 2).
				if latest.AtMain() {
					return true, nil
				}
				first, _ := landing.RootBudget(state, r.task)
				if now.Before(first.Add(landingHoldLimit)) {
					return true, nil
				}
				continue
			}
			if !handoff || r.accepted == nil || r.accepted.Before(state.Fence()) {
				continue
			}
			if now.Before(r.accepted.Add(landing.ClaimWindow)) {
				held = true
				continue
			}
			// Its window is over with no record: record it, then let go.
			// The pass's fold cache never holds this write: state above was
			// folded from committed rows before it, and the stream is dropped
			// from the cache here, so whether this mission's transaction then
			// commits or rolls back (the savepoint, MoveMission), the next
			// mission folds the stream again from what is committed (review
			// 652 R652-1).
			recorded, err := failUnclaimedLocked(ctx, tx, stream, mission, r.task, "bound", "its window ended unclaimed; recorded by the close")
			if err != nil {
				delete(folds, stream)
				return false, err
			}
			if recorded {
				delete(folds, stream)
			}
		}
	}
	return held, nil
}

// landingFolds keeps, for one CloseMissions pass, each landing stream's fold
// with the stream's head seq it was folded at (TD-447 3: every closable
// mission folded its repository's whole stream again under its lock). The
// stream is append-only and appends to it take its lock in order, so an
// unchanged head means unchanged contents: the fold is reused. A new head is
// folded again. Callers only read the State.
type landingFolds map[string]landingFold

type landingFold struct {
	head  int64
	state landing.State
}

func (m landingFolds) state(ctx context.Context, tx pgx.Tx, stream string) (landing.State, error) {
	var head int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) FROM agentrt.event WHERE event_stream_id = $1`, stream).Scan(&head); err != nil {
		return landing.State{}, err
	}
	if f, ok := m[stream]; ok && f.head == head {
		return f.state, nil
	}
	hook("landing_fold", stream)
	s, err := LandingState(ctx, tx, stream)
	if err != nil {
		return landing.State{}, err
	}
	m[stream] = landingFold{head: head, state: s}
	hook("landing_folded", stream)
	return s, nil
}
