package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// A controller loop that keeps failing the same way (TD-411: STG's facts
// pass failed every 15 seconds for two hours on a check constraint its
// database's schema lacked, logged as a warning that nobody read). The
// controller tells it after FailuresToAlert failures in a row with the same
// error: a policy_alert on the controller stream, rule controller_failing,
// the loop named by line_digest (lineDigest of its name), its error kept as a
// log_tail artifact; once the loop passes again, a note there says so.
// Progress lists the loops whose latest record is an alert, and the Console
// says so where the Owner looks (Work, System). The alert is recorded once a
// run of failures, again only after the error changes.

// ControllerStream is the controller's own stream.
const ControllerStream = "controller"

// FailuresToAlert is how many failures in a row with the same error make an
// alert: a minute of the facts loop, four seconds of the card rule's.
const FailuresToAlert = 4

// controllerFailingPhrase is the alert's phrase.
const controllerFailingPhrase = "a controller loop failed the same way several times in a row"

// ControllerLoops are the controller's loops (controller.Start), the names an
// alert's line_digest is read back to.
var ControllerLoops = []string{"agentrt-leases", "agentrt-facts-rules", "agentrt-card-rule", "agentrt-assess", "agentrt-landings", "agentrt-disk-pages",
	"agentrt-presence"}

// errorKept bounds the error an alert keeps.
const errorKept = 4096

// AlertFailingLoop records that loop failed n times in a row with errText.
func AlertFailingLoop(ctx context.Context, db DB, loop string, n int, errText string) error {
	text := fmt.Sprintf("%s failed %d times in a row:\n%s", loop, n, errText)
	if len(text) > errorKept {
		cut := errorKept
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "…"
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		art, err := PutArtifact(ctx, tx, "controller", "log_tail", "text/plain; charset=utf-8", []byte(text))
		if err != nil {
			return err
		}
		_, err = Append(ctx, tx, Event{Stream: ControllerStream, Type: "policy_alert", Actor: "controller",
			Body: map[string]any{"rule": "controller_failing", "source": "controller", "phrase": controllerFailingPhrase,
				"artifact_id": art, "line_digest": lineDigest(loop)}})
		return err
	})
}

// NoteLoopPasses records that loop passed again after n failures in a row
// (n 0: the count is not known, as after a restart).
func NoteLoopPasses(ctx context.Context, db DB, loop string, n int) error {
	text := loop + passesAgain
	if n > 0 {
		text += fmt.Sprintf(" after %d failures in a row", n)
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: ControllerStream, Type: "note", Actor: "controller", Body: map[string]any{"text": text}})
		return err
	})
}

// passesAgain follows the loop's name in the note that it passes again.
const passesAgain = " passes again"

// loopRecord is a loop's latest record on the controller stream: an alert
// (when, and the artifact of its error) or the note that it passes again.
type loopRecord struct {
	found, alert bool
	at           time.Time
	artifact     string
}

// latestLoopRecord reads loop's latest record: the controller's
// controller_failing alert naming it, or its note that it passes again
// (review 466 B-1: by loop, not out of a window of the whole stream, which
// another loop's alerts and notes could fill).
func latestLoopRecord(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, loop string) (loopRecord, error) {
	var r loopRecord
	var kind string
	err := q.QueryRow(ctx, `SELECT event_type, at, COALESCE(body_json->>'artifact_id', '') FROM agentrt.event
	  WHERE event_stream_id = $1 AND actor_id = 'controller' AND (
	    (event_type = 'policy_alert' AND body_json->>'rule' = 'controller_failing' AND body_json->>'line_digest' = $2)
	    OR (event_type = 'note' AND starts_with(body_json->>'text', $3)))
	  ORDER BY seq DESC LIMIT 1`, ControllerStream, lineDigest(loop), loop+passesAgain).Scan(&kind, &r.at, &r.artifact)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	r.found, r.alert = err == nil, kind == "policy_alert"
	return r, err
}

// LoopAlertOpen says whether loop's latest record is an alert no note has
// answered: a loop starting again (a restart) writes the note once it passes
// (review 466 A-1).
func LoopAlertOpen(ctx context.Context, db DB, loop string) (bool, error) {
	var open bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		r, err := latestLoopRecord(ctx, tx, loop)
		open = r.alert
		return err
	})
	return open, err
}

// failingLoops are the loops whose latest record on the controller stream is
// an alert: the loop, when it was alerted and its error as kept, oldest
// alert first.
func failingLoops(ctx context.Context, tx pgx.Tx) ([]map[string]any, error) {
	type alert struct {
		loop string
		rec  loopRecord
	}
	var failing []alert
	for _, l := range ControllerLoops {
		r, err := latestLoopRecord(ctx, tx, l)
		if err != nil {
			return nil, err
		}
		if r.alert {
			failing = append(failing, alert{l, r})
		}
	}
	sort.SliceStable(failing, func(i, j int) bool { return failing[i].rec.at.Before(failing[j].rec.at) })
	out := make([]map[string]any, 0, len(failing))
	for _, a := range failing {
		var body []byte
		if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, a.rec.artifact).Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"loop": a.loop, "since": a.rec.at.UTC().Format(time.RFC3339Nano), "error": string(body)})
	}
	return out, nil
}
