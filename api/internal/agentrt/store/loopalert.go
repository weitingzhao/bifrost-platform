package store

import (
	"context"
	"fmt"
	"strings"
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
var ControllerLoops = []string{"agentrt-leases", "agentrt-facts-rules", "agentrt-card-rule", "agentrt-assess", "agentrt-landings"}

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

// NoteLoopPasses records that loop passed again after n failures in a row.
func NoteLoopPasses(ctx context.Context, db DB, loop string, n int) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: ControllerStream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": fmt.Sprintf("%s passes again after %d failures in a row", loop, n)}})
		return err
	})
}

// failingWindow bounds the controller stream's records read for Progress:
// two per loop and run, far more than a loop's latest needs.
const failingWindow = 200

// failingLoops are the loops whose latest record on the controller stream is
// an alert: the loop, when it was alerted and its error as kept, oldest
// alert first.
func failingLoops(ctx context.Context, tx pgx.Tx) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT event_type, at, COALESCE(body_json->>'line_digest', ''), COALESCE(body_json->>'text', ''),
	    COALESCE(body_json->>'artifact_id', '') FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type IN ('policy_alert','note') ORDER BY seq DESC LIMIT $2`, ControllerStream, failingWindow)
	if err != nil {
		return nil, err
	}
	type alert struct {
		loop     string
		at       time.Time
		artifact string
	}
	byDigest := map[string]string{}
	for _, l := range ControllerLoops {
		byDigest[lineDigest(l)] = l
	}
	decided := map[string]bool{}
	var failing []alert
	for rows.Next() {
		var kind, digest, text, artifact string
		var at time.Time
		if err = rows.Scan(&kind, &at, &digest, &text, &artifact); err != nil {
			rows.Close()
			return nil, err
		}
		loop := byDigest[digest]
		if kind == "note" {
			if i := strings.Index(text, " passes again"); i > 0 {
				loop = text[:i]
			}
		}
		if loop == "" || decided[loop] {
			continue
		}
		decided[loop] = true
		if kind == "policy_alert" {
			failing = append(failing, alert{loop, at, artifact})
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(failing))
	for i := len(failing) - 1; i >= 0; i-- {
		a := failing[i]
		var body []byte
		if err = tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, a.artifact).Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"loop": a.loop, "since": a.at.UTC().Format(time.RFC3339Nano), "error": string(body)})
	}
	return out, nil
}
