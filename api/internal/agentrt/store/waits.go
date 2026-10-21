package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// Waits past their limit (W-31 2.3; RULE-waits-2026-10-12 总则 1–4, design
// v5 §5.3 「等待计时、看护 → 控制器的 wait_limit_passed」): the platform keeps the
// clock of the waits the runtime itself knows, not a thread or an Agent host,
// so a wait is timed while the thread is gone and the host asleep. Each pass
// records, once a wait, a wait_limit_passed for:
//
//   - attempt_running (RULE-waits classes 1 and 2): an attempt running past
//     twice its task's usual time, and at least the floor of its kind: 30
//     minutes for a plan, an implementation or other work, 20 for a
//     verification, a review or a brief. The usual time is the one the task
//     was proposed with, else the median of its role and class's accepted
//     attempts (the Progress projection's, usualTimes), else only the floor.
//     A stale attempt is the lease loop's, not timed here.
//   - task_ready (class 7): a ready task no host has claimed, its mission
//     active and no stop or pause holding it, past twice its usual time and
//     at least 30 minutes (the class's "twice the usual time of the one in
//     front" taken as its own role and class's).
//   - message_unread (class 3): a message to an interactive thread it has not
//     acknowledged after 10 minutes, for at most WaitMessageHorizon.
//
// The Owner's waits (classes 4 and 5) are timed by the nudges (briefdue.go),
// and the runtime records nothing of external waits (class 6) in round 2.
// The record is the first of the rule's three steps; it names whom the wait
// calls (calls) and changes nothing else: no state, no message, no notice.
// One wait is told apart from its record: a brief of the chief of staff's
// ready past its limit is told to the Owner by the platform, for the seat
// that would tell him is what does not run (briefwait.go; review 487 A1).

// WaitsStream is the stream the records are on, apart from the missions':
// a record's references (its mission, task, attempt) are checked at its
// commit, which waits for a transaction holding one of those rows, and
// such a transaction (a start, a checkpoint, a submit: the attempt's row,
// then its mission's stream) never waits for this stream, so the two never
// deadlock.
const WaitsStream = "waits"

// The waits.
const (
	WaitAttemptRunning = "attempt_running"
	WaitTaskReady      = "task_ready"
	WaitMessageUnread  = "message_unread"
)

// The limits (RULE-waits §2): a limit is WaitFactor times the usual time,
// and never below its floor.
const (
	WaitFactor         = 2
	WaitWorkFloor      = 30 * time.Minute // a plan, an implementation, other work; a ready task
	WaitCheckFloor     = 20 * time.Minute // a verification, a review, a brief
	WaitMessageLimit   = 10 * time.Minute
	WaitMessageHorizon = 7 * 24 * time.Hour // older unread mail is no longer a wait anyone is in
	waitsPerPass       = 20                 // of each wait, the oldest first; the rest on the next pass
	// A pass is bounded in time and in each lock it waits for (review 474
	// B3): a record waits at its commit for a row another transaction
	// holds; past the bound the pass fails, its loop says so, and the next
	// pass tries again.
	waitsPassTimeout = 30 * time.Second
	waitsLockTimeout = "5s"
	// A record is written at least its limit (10 minutes or more) after its
	// wait began, so the records of a wait are looked for from a day before
	// it began: the (event_type, at) index bounds the look-up whatever the
	// clocks' skew (review 474 B1).
	waitsLookBack = "1 day"
)

// What a wait's limit came from.
const (
	basisTaskUsual    = "task_usual"
	basisHistoryUsual = "history_usual"
	basisFloor        = "floor"
)

// A passed wait, read to be recorded: its limit and what the limit came
// from are the query's (waitLimit), so the record says the limit it passed.
type passedWait struct {
	wait, ref, mission, task, attempt, calls, basis string
	since                                           time.Time
	limitS                                          int64
	usualS                                          *float64
}

// WaitLimits is one pass: it records the waits that passed their limit at
// now and were not recorded yet, and returns how many it recorded.
func WaitLimits(ctx context.Context, db DB, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, waitsPassTimeout)
	defer cancel()
	var found []passedWait
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		found = nil
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+waitsLockTimeout+`'`); err != nil {
			return err
		}
		usual, err := usualTimes(ctx, tx, now)
		if err != nil {
			return err
		}
		keys, secs := make([]string, 0, len(usual)), make([]float64, 0, len(usual))
		for k, u := range usual {
			keys, secs = append(keys, k), append(secs, u.d.Seconds())
		}
		for _, q := range []struct {
			wait string
			sql  string
			args []any
		}{
			{WaitAttemptRunning, attemptsPastLimit, []any{now, keys, secs, float64(WaitFactor), waitsPerPass, WaitCheckFloor.Seconds(), WaitWorkFloor.Seconds()}},
			{WaitTaskReady, readyPastLimit, []any{now, keys, secs, float64(WaitFactor), waitsPerPass, WaitWorkFloor.Seconds()}},
			{WaitMessageUnread, mailPastLimit, []any{now, int64(WaitMessageLimit / time.Second), int64(WaitMessageHorizon / time.Second), waitsPerPass}},
		} {
			rows, err := tx.Query(ctx, q.sql, q.args...)
			if err != nil {
				return err
			}
			list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (passedWait, error) {
				p := passedWait{wait: q.wait}
				return p, r.Scan(&p.ref, &p.mission, &p.task, &p.attempt, &p.since, &p.limitS, &p.basis, &p.usualS, &p.calls)
			})
			if err != nil {
				return err
			}
			found = append(found, list...)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	recorded := 0
	var errs []error
	for _, p := range found {
		did, err := recordWait(ctx, db, p, now)
		if err != nil {
			errs = append(errs, err)
		}
		if did {
			recorded++
		}
	}
	return recorded, errors.Join(errs...)
}

// recordWait records p unless it is on record already: under the waits
// stream's lock, so two passes at once record it once.
func recordWait(ctx context.Context, db DB, p passedWait, now time.Time) (bool, error) {
	body := map[string]any{"wait": p.wait, "ref": p.ref, "since": p.since.UTC().Format(time.RFC3339Nano),
		"limit_s": p.limitS, "basis": p.basis, "waited_s": int64(now.Sub(p.since) / time.Second), "calls": p.calls}
	if p.usualS != nil {
		body["usual_s"] = int64(math.Round(*p.usualS))
	}
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+waitsLockTimeout+`'`); err != nil {
			return err
		}
		if err := lockStream(ctx, tx, WaitsStream); err != nil {
			return err
		}
		var seen bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event w WHERE w.event_type = 'wait_limit_passed'
		  AND w.at >= $3::timestamptz - interval '`+waitsLookBack+`'
		  AND w.body_json->>'wait' = $1 AND w.body_json->>'ref' = $2 AND (w.body_json->>'since')::timestamptz = $3)`,
			p.wait, p.ref, p.since).Scan(&seen); err != nil || seen {
			return err
		}
		if _, err := Append(ctx, tx, Event{Stream: WaitsStream, Type: "wait_limit_passed", Actor: "controller",
			MissionID: p.mission, TaskID: p.task, AttemptID: p.attempt, Body: body}); err != nil {
			return err
		}
		did = true
		return nil
	})
	// A record whose commit failed is not one (review 474 B2).
	return did && err == nil, err
}

// The calls of a task's wait: whoever proposed the work it is part of, the
// work a check checks for a check, the mission's proposer for what the
// controller proposed.
const waitCalls = `COALESCE(NULLIF(s.created_by_actor_id, 'controller'), NULLIF(t.created_by_actor_id, 'controller'), m.created_by_actor_id)`

// The usual times as a relation, from the arrays of keys (role|class) and
// seconds usualTimes gave ($2, $3).
const usualRelation = `u AS (SELECT * FROM unnest($2::text[], $3::float8[]) AS x(k, secs))`

// waitLimit is the limit of a candidate c with its floor_s, usual_s and
// from_task, $4 the factor: whole seconds, the factor times the usual time
// rounded up and never below the floor; and what it came from.
const waitLimit = `GREATEST(c.floor_s, ceil($4::float8 * COALESCE(c.usual_s, 0)))::bigint AS limit_s,
	    CASE WHEN c.usual_s IS NULL OR ceil($4::float8 * c.usual_s) <= c.floor_s THEN '` + basisFloor + `'
	      WHEN c.from_task THEN '` + basisTaskUsual + `' ELSE '` + basisHistoryUsual + `' END AS basis`

// attemptsPastLimit: $1 now, $2 and $3 the usual times, $4 the factor, $5
// the bound, $6 a check's floor, $7 other work's. Columns as passedWait
// scans them.
const attemptsPastLimit = `WITH ` + usualRelation + `,
	c AS (SELECT a.attempt_id, t.task_id, t.mission_id, a.started_at AS since,
	    CASE WHEN t.role IN ('verify','review','brief') THEN $6::float8 ELSE $7::float8 END AS floor_s,
	    COALESCE(t.usual_s::float8, u.secs) AS usual_s, t.usual_s IS NOT NULL AS from_task, ` + waitCalls + ` AS calls
	  FROM agentrt.attempt a
	  JOIN agentrt.task t ON t.task_id = a.task_id
	  JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  LEFT JOIN agentrt.task s ON s.task_id = t.subject_task_id
	  LEFT JOIN u ON u.k = t.role || '|' || t.task_class
	  WHERE a.state = 'running' AND a.started_at IS NOT NULL),
	l AS (SELECT c.*, ` + waitLimit + ` FROM c)
	SELECT c.attempt_id, c.mission_id, c.task_id, c.attempt_id, c.since, c.limit_s, c.basis, c.usual_s, c.calls FROM l c
	WHERE extract(epoch FROM $1::timestamptz - c.since) > c.limit_s
	  AND NOT EXISTS (SELECT 1 FROM agentrt.event w WHERE w.task_id = c.task_id AND w.event_type = 'wait_limit_passed'
	    AND w.body_json->>'wait' = 'attempt_running' AND w.body_json->>'ref' = c.attempt_id)
	ORDER BY c.since, c.attempt_id LIMIT $5`

// readyPastLimit: $1 now, $2 and $3 the usual times, $4 the factor, $5 the
// bound, $6 the floor. A ready task's wait began when it last became ready.
const readyPastLimit = `WITH ` + usualRelation + `,
	c AS (SELECT t.task_id, t.mission_id,
	    (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id = t.task_id AND e.event_type = 'task_state_changed'
	      AND e.body_json->>'to' = 'ready') AS since,
	    $6::float8 AS floor_s, COALESCE(t.usual_s::float8, u.secs) AS usual_s, t.usual_s IS NOT NULL AS from_task, ` + waitCalls + ` AS calls
	  FROM agentrt.task t
	  JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  LEFT JOIN agentrt.task s ON s.task_id = t.subject_task_id
	  LEFT JOIN u ON u.k = t.role || '|' || t.task_class
	  WHERE t.state = 'ready' AND m.state = 'active'
	    AND NOT EXISTS (SELECT 1 FROM agentrt.control k WHERE k.cleared_at IS NULL AND k.control_kind IN ('stop','pause')
	      AND (k.scope = 'global' OR (k.scope = 'mission' AND k.scope_ref = t.mission_id)
	        OR (k.scope = 'task' AND (k.scope_ref = t.task_id OR k.scope_ref = t.subject_task_id))))),
	l AS (SELECT c.*, ` + waitLimit + ` FROM c WHERE c.since IS NOT NULL)
	SELECT c.task_id, c.mission_id, c.task_id, '', c.since, c.limit_s, c.basis, c.usual_s, c.calls FROM l c
	WHERE extract(epoch FROM $1::timestamptz - c.since) > c.limit_s
	  AND NOT EXISTS (SELECT 1 FROM agentrt.event w WHERE w.task_id = c.task_id AND w.event_type = 'wait_limit_passed'
	    AND w.body_json->>'wait' = 'task_ready' AND (w.body_json->>'since')::timestamptz = c.since)
	ORDER BY c.since, c.task_id LIMIT $5`

// mailPastLimit: $1 now, $2 the limit, $3 the horizon, $4 the bound. A
// message's wait is its thread's; it has no usual time.
const mailPastLimit = `SELECT m.message_id, COALESCE(m.mission_id, ''), COALESCE(m.task_id, ''), '', m.created_at,
	  $2::bigint, '` + basisFloor + `', NULL::float8, m.to_actor_id
	FROM agentrt.message m
	WHERE m.acked_at IS NULL AND m.to_actor_id LIKE 'thread:%'
	  AND m.created_at < $1::timestamptz - make_interval(secs => $2::bigint)
	  AND m.created_at > $1::timestamptz - make_interval(secs => $3::bigint)
	  AND NOT EXISTS (SELECT 1 FROM agentrt.event w WHERE w.event_type = 'wait_limit_passed'
	    AND w.at >= m.created_at - interval '` + waitsLookBack + `'
	    AND w.body_json->>'wait' = 'message_unread' AND w.body_json->>'ref' = m.message_id)
	ORDER BY m.created_at, m.message_id LIMIT $4`
