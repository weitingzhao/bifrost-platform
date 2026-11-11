package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// HandinRefusalReason is the bounded reason recorded without request content.
func HandinRefusalReason(err error) string {
	switch {
	case errors.Is(err, ErrPinNotPermitted), errors.Is(err, ErrNotPinned), errors.Is(err, ErrNotExternal):
		return "pin_not_permitted"
	case errors.Is(err, ErrMixedSource):
		return "mixed_source_not_supported"
	case errors.Is(err, ErrCoreMismatch):
		return "core_mismatch"
	}
	return ""
}

// RecordHandinRefusal commits independently of the refused write. Append supplies
// the runtime timestamp; task is a proposal reference when no task exists yet.
func RecordHandinRefusal(ctx context.Context, db DB, task, attempt string, refusal error) error {
	reason := HandinRefusalReason(refusal)
	if reason == "" {
		return nil
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		taskID := ""
		if attempt != "" {
			if err := tx.QueryRow(ctx, "SELECT task_id FROM agentrt.attempt WHERE attempt_id = $1", attempt).Scan(&task); err != nil {
				return err
			}
			taskID = task
		}
		ev := handinRefusalEvent(task, taskID, reason)
		_, err := Append(ctx, tx, ev)
		return err
	})
}

// handinRefusal carries only the rejected proposal's local reference.
type handinRefusal struct {
	task string
	err  error
}

func (e *handinRefusal) Error() string { return e.err.Error() }
func (e *handinRefusal) Unwrap() error { return e.err }

// HandinRefusalTask returns the rejected task, or the first pinned reference for
// batch-wide authorization failures.
func HandinRefusalTask(err error, tasks []PinnedTask) string {
	var refusal *handinRefusal
	if errors.As(err, &refusal) {
		return refusal.task
	}
	for _, t := range tasks {
		if t.ExecutorPin != "" {
			return t.Ref
		}
	}
	return ""
}

// The note vocabulary stays unchanged: text holds a small, queryable JSON
// object. The event envelope carries the recorder's timestamp.
func handinRefusalEvent(task, taskID, reason string) Event {
	body, _ := json.Marshal(map[string]string{"task": task, "reason": reason})
	return Event{Stream: "handin-refusals", Type: "note", Actor: "controller", TaskID: taskID, Body: map[string]any{"text": string(body)}}
}
