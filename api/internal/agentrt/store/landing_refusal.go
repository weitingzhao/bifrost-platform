package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A claim the landing loop refuses writes no landing record and returns no
// error: it only waits and tries again (TD-447 5). On 2026-10-28 the first
// live landing (M-181) sat refused as stale for ten minutes with nothing to
// show why (CUT §11.13). landing_refusal records the reason on the
// repository stream, once per change of reason for a task, so the mission's
// landing status and the goal page can say why a root waits.

// claimRefusalReasons are the reasons a refused claim records.
var claimRefusalReasons = []string{"mode", "before_enable", "stale", "result", "history", "terminal", "floor",
	"dependencies", "rule", "limit", "stage", "facts_unreadable", "stopped", "bound", "in_progress"}

// RecordClaimRefusal records why the claim of task was refused, unless the
// task's latest refusal already says the same. It reports whether it wrote.
func RecordClaimRefusal(ctx context.Context, db DB, repo, task, reason string) (bool, error) {
	if !slices.Contains(claimRefusalReasons, reason) {
		return false, nil // not a refusal this record speaks for (changed, rehearsed)
	}
	stream := "landing:" + repo
	wrote := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		wrote = false
		last, _, err := latestClaimRefusal(ctx, tx, stream, task)
		if err != nil || last == reason {
			return err
		}
		_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: task,
			Body: map[string]any{"text": "Landing claim refused: " + reason, "landing_refusal": map[string]any{"v": "1", "task": task, "reason": reason}}})
		wrote = err == nil
		return err
	})
	return wrote, err
}

// latestClaimRefusal is the task's latest recorded refusal, or "".
func latestClaimRefusal(ctx context.Context, tx pgx.Tx, stream, task string) (string, time.Time, error) {
	var reason string
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_refusal'->>'reason', at FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json->'landing_refusal'->>'task' = $2 ORDER BY seq DESC LIMIT 1`, stream, task).Scan(&reason, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, nil
	}
	return reason, at, err
}

// badLandingRefusal is why a landing_refusal body is refused, or "".
func badLandingRefusal(v any) string {
	m, ok := v.(map[string]any)
	if !ok || m["v"] != "1" {
		return "landing_refusal is not a v1 object"
	}
	if !landingRoot.MatchString(fmt.Sprint(m["task"])) {
		return "landing_refusal task is not a task"
	}
	if r, _ := m["reason"].(string); !slices.Contains(claimRefusalReasons, r) {
		return "landing_refusal reason is not a claim refusal"
	}
	return ""
}

// checkLandingRefusalAppend: the controller writes it on a repository
// landing stream, under its task, never as a landing record.
func checkLandingRefusalAppend(ev Event) error {
	v, ok := ev.Body["landing_refusal"]
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !strings.HasPrefix(ev.Stream, "landing:") || m == nil || ev.TaskID != fmt.Sprint(m["task"]) {
		return fmt.Errorf("landing_refusal is written by the controller on a repository landing stream under its task")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("landing_refusal is not a landing record")
	}
	return nil
}
