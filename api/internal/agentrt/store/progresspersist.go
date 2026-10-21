package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// What of an attempt's work is safe on the platform (TD-352, design v5 §5.2
// T34 ⑤; Gate A 2026-10-15 STG M-5: gate-b checkpointed once and went away
// for good, and Progress said only maybe_stuck, "the lease lapsed and was not
// renewed"). A live or lost attempt says whether its result is persisted,
// the last checkpoint the platform confirmed (its seq and when) and how long
// nothing of it has been persisted; a stale attempt, an attempt on a lost
// host and a lost attempt whose result is not persisted say "result not
// persisted" in their reason, and none of them reads as done.

// checkpointOf is the SQL of an attempt a's last confirmed checkpoint: its
// seq and when (the event, by the task's index).
const checkpointOf = `LEFT JOIN LATERAL (SELECT (e.body_json->>'checkpoint_seq')::bigint AS seq, e.at FROM agentrt.event e
	    WHERE e.task_id = a.task_id AND e.attempt_id = a.attempt_id AND e.event_type = 'attempt_checkpointed'
	    ORDER BY e.event_id DESC LIMIT 1) cp ON true`

// keep records what of a's work is persisted: its result (persisted), its
// last checkpoint (seq and at, nil for none) and, when its result is not
// persisted, how long since anything of it was: since that checkpoint, or
// since it started.
func (a *ProgressAttempt) keep(persisted bool, seq *int64, at *time.Time, now time.Time) {
	a.Persisted = persisted
	a.LastCheckpointSeq = seq
	since := a.started
	if at != nil {
		a.LastCheckpointAt = at.UTC().Format(time.RFC3339Nano)
		since = *at
	}
	if !persisted && !since.IsZero() {
		s := int64(now.Sub(since) / time.Second)
		a.UnpersistedForS = &s
	}
}

// notPersisted is what a reason adds for an attempt whose result is not
// persisted, "" for one whose is.
func notPersisted(a *ProgressAttempt) string {
	if a.Persisted {
		return ""
	}
	var what string
	switch {
	case a.LastCheckpointSeq != nil:
		what = fmt.Sprintf("last checkpoint %d at %s", *a.LastCheckpointSeq, a.LastCheckpointAt)
	case a.started.IsZero():
		return "; result not persisted (it never started)"
	default:
		what = "no checkpoint"
	}
	if a.UnpersistedForS != nil {
		what += ", nothing persisted for " + spanWords(*a.UnpersistedForS)
	}
	return "; result not persisted (" + what + ")"
}

// spanWords says a span of seconds as seconds, or whole minutes from two.
func spanWords(s int64) string {
	if s < 120 {
		return fmt.Sprintf("%d s", s)
	}
	return fmt.Sprintf("%d min", s/60)
}

// lostAttempts reads, for the unfinished tasks of the missions, each task's
// latest lost attempt that no later attempt's persisted result has
// superseded, with what of it is persisted. Its reason says when it was lost
// and what was lost with it; it has no status, being no live attempt.
func lostAttempts(ctx context.Context, tx pgx.Tx, missions []string, now time.Time) (map[string]*ProgressAttempt, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (a.task_id) a.task_id, a.attempt_id, a.attempt_no, t.mission_id, COALESCE(a.host_id, ''),
	    a.executor_actor_id, COALESCE(a.model, ''), a.started_at, a.ended_at, a.heartbeat_at, a.persisted_at IS NOT NULL, cp.seq, cp.at
	  FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
	  `+checkpointOf+`
	  WHERE t.mission_id = ANY($1) AND t.state NOT IN ('accepted','rejected','cancelled') AND a.state = 'lost'
	    AND NOT EXISTS (SELECT 1 FROM agentrt.attempt b WHERE b.task_id = a.task_id AND b.attempt_no > a.attempt_no
	      AND b.persisted_at IS NOT NULL)
	  ORDER BY a.task_id, a.attempt_no DESC`, missions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ProgressAttempt{}
	for rows.Next() {
		a := &ProgressAttempt{State: "lost"}
		var started, ended, at *time.Time
		var heartbeat time.Time
		var persisted bool
		var seq *int64
		if err = rows.Scan(&a.TaskID, &a.AttemptID, &a.AttemptNo, &a.MissionID, &a.HostID, &a.Executor, &a.Model, &started, &ended,
			&heartbeat, &persisted, &seq, &at); err != nil {
			return nil, err
		}
		a.HeartbeatAgeS = int64(now.Sub(heartbeat) / time.Second)
		if started != nil {
			a.started = *started
			a.StartedAt = started.UTC().Format(time.RFC3339Nano)
		}
		a.EndedAt = stamp(ended)
		a.keep(persisted, seq, at, now)
		a.Reason = fmt.Sprintf("attempt %d", a.AttemptNo)
		if a.HostID != "" {
			a.Reason += " on " + a.HostID
		}
		a.Reason += " was lost"
		if a.EndedAt != "" {
			a.Reason += " at " + a.EndedAt
		}
		if persisted {
			a.Reason += "; its result was persisted after (salvaged)"
		} else {
			a.Reason += notPersisted(a)
		}
		out[a.TaskID] = a
	}
	return out, rows.Err()
}
