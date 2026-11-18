// Package loopreadings projects recorded loop failures without starting loops or writing alerts.
package loopreadings

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
)

// Record is a loop's latest record on the controller stream: an alert
// (when, and the artifact of its error) or the note that it passes again.
type Record struct {
	Found, Alert bool
	At           time.Time
	Artifact     string
}

// Latest reads loop's latest record: the controller's alert naming it
// (controller_loop_failing; before 2026-11-17 a policy_alert of rule
// controller_failing) or the record that it passes again
// (controller_loop_recovered; before, a note starting passedPrefix)
// (review 466 B-1: by loop, not out of a window of the whole stream, which
// another loop's alerts and notes could fill).
func Latest(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, stream, digest, passedPrefix string) (Record, error) {
	var r Record
	var kind string
	err := q.QueryRow(ctx, `SELECT event_type, at, COALESCE(body_json->>'artifact_id', '') FROM agentrt.event
	  WHERE event_stream_id = $1 AND actor_id = 'controller' AND (
	    (event_type IN ('controller_loop_failing', 'controller_loop_recovered') AND body_json->>'line_digest' = $2)
	    OR (event_type = 'policy_alert' AND body_json->>'rule' = 'controller_failing' AND body_json->>'line_digest' = $2)
	    OR (event_type = 'note' AND starts_with(body_json->>'text', $3)))
	  ORDER BY seq DESC LIMIT 1`, stream, digest, passedPrefix).Scan(&kind, &r.At, &r.Artifact)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	r.Found, r.Alert = err == nil, kind == "controller_loop_failing" || kind == "policy_alert"
	return r, err
}

// Failing lists the loops whose latest record on the controller stream is
// an alert: the loop, when it was alerted and its error as kept, oldest
// alert first.
func Failing(ctx context.Context, tx pgx.Tx, loops []string, latest func(context.Context, pgx.Tx, string) (Record, error)) ([]map[string]any, error) {
	type alert struct {
		loop string
		rec  Record
	}
	var failing []alert
	for _, l := range loops {
		r, err := latest(ctx, tx, l)
		if err != nil {
			return nil, err
		}
		if r.Alert {
			failing = append(failing, alert{l, r})
		}
	}
	sort.SliceStable(failing, func(i, j int) bool { return failing[i].rec.At.Before(failing[j].rec.At) })
	out := make([]map[string]any, 0, len(failing))
	for _, a := range failing {
		var body []byte
		if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, a.rec.Artifact).Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"loop": a.loop, "since": a.rec.At.UTC().Format(time.RFC3339Nano), "error": string(body)})
	}
	return out, nil
}
