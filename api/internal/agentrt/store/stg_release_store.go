package store

import (
	"context"
	"maps"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// StgReleaseState reads only the authoritative repository stream. A writer
// calls it after lockStream/Append has acquired the stream lock, in that same
// transaction. Read callers receive a detached, replayed snapshot.
func StgReleaseState(ctx context.Context, tx pgx.Tx) (StgState, error) {
	rows, err := tx.Query(ctx, `SELECT seq, at, event_type, actor_id, body_json, COALESCE(task_id,''), COALESCE(mission_id,''), COALESCE(plan_node_id,''), COALESCE(attempt_id,'') FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq`, StgReleaseStream)
	if err != nil {
		return StgState{}, err
	}
	defer rows.Close()
	entries := []StgEntry{}
	for rows.Next() {
		var e StgEntry
		var raw []byte
		e.Event.Stream = StgReleaseStream
		if err := rows.Scan(&e.Seq, &e.At, &e.Event.Type, &e.Event.Actor, &raw, &e.Event.TaskID, &e.Event.MissionID, &e.Event.PlanNode, &e.Event.AttemptID); err != nil {
			return StgState{}, err
		}
		v, err := canon.Parse(raw)
		if err != nil {
			return StgState{}, err
		}
		e.Event.Body, _ = v.(map[string]any)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return StgState{}, err
	}
	return StgFold(entries)
}

// checkStgReleaseAppend runs after the stream lock and before hashing or
// inserting anything. The caller's intent/spend are placeholders: the recorder
// replaces them, together with the intent annotation and its fingerprint.
// A same-UID bound is the sole no-write idempotent edge.
func checkStgReleaseAppend(ctx context.Context, tx pgx.Tx, ev *Event, seq int64, at time.Time) (*Appended, error) {
	_, ok := ev.Body["stg_release"]
	if !ok {
		return nil, nil
	}
	s, err := StgReleaseState(ctx, tx)
	if err != nil {
		return nil, err
	}
	r, idempotent, err := checkStgReleaseEvent(s, *ev, seq, at)
	if err != nil {
		return nil, err
	}
	if idempotent > 0 {
		var old Appended
		err := tx.QueryRow(ctx, `SELECT event_id,seq,sha256,at FROM agentrt.event WHERE event_stream_id=$1 AND seq=$2`, StgReleaseStream, idempotent).Scan(&old.EventID, &old.Seq, &old.SHA256, &old.At)
		// Same shape as a fresh Append: the recorder's time is always UTC.
		old.At = old.At.UTC()
		return &old, err
	}
	ev.Body = maps.Clone(ev.Body)
	ev.Body["stg_release"] = map[string]any(r)
	return nil, nil
}

// checkStgReleaseEvent is the exact no-I/O portion of Append's locked check.
// It is also exercised without a database by the event matrix tests.
func checkStgReleaseEvent(s StgState, ev Event, seq int64, at time.Time) (StgRecord, int64, error) {
	if err := stgEnvelope(ev); err != nil {
		return nil, 0, err
	}
	r, err := ParseStgRelease(ev.Body["stg_release"])
	if err != nil {
		return nil, 0, err
	}
	if r.String("kind") == "started" {
		b := s.Batches[r.String("batch")]
		if b == nil || b.Phase != "SNAP" || b.Try != r.Int("try") {
			return nil, 0, stgError("transition", "started", "requires the current snapshot")
		}
		spend, err := s.expectedSpend(b)
		if err != nil {
			return nil, 0, err
		}
		r["intent_seq"] = seq
		r["spend"] = spend
		annotations := r.object("annotations")
		annotations["bifrost.io/stg-release-intent"] = strconv.FormatInt(seq, 10)
		r["fingerprint"] = stgFingerprint(b.Tries[b.Try].Snapshot.Record.object("inputs"), r.object("labels"), annotations)
	}
	idempotent, err := s.apply(StgEvent{seq, at, r})
	return r, idempotent, err
}
