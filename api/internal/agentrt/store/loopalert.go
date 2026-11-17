package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/loopreadings"
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
	"agentrt-presence", "agentrt-slots"}

// errorKept bounds the error an alert keeps.
const errorKept = 4096

// AlertFailingLoop records that loop failed n times in a row with errText.
func AlertFailingLoop(ctx context.Context, db DB, loop string, n int, errText string) error {
	text := loopreadings.FailureText(loop, n, errText, errorKept)
	return InTx(ctx, db, func(tx pgx.Tx) error {
		art, err := artifact.PutArtifact(ctx, tx, "controller", "log_tail", "text/plain; charset=utf-8", []byte(text))
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
	text := loopreadings.RecoveryText(loop, n, passesAgain)
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: ControllerStream, Type: "note", Actor: "controller", Body: map[string]any{"text": text}})
		return err
	})
}

// passesAgain follows the loop's name in the note that it passes again.
const passesAgain = " passes again"

type loopRecord struct {
	found, alert bool
	at           time.Time
	artifact     string
}

func latestLoopRecord(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, loop string) (loopRecord, error) {
	r, err := loopreadings.Latest(ctx, q, ControllerStream, lineDigest(loop), loop+passesAgain)
	return loopRecord{found: r.Found, alert: r.Alert, at: r.At, artifact: r.Artifact}, err
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

func failingLoops(ctx context.Context, tx pgx.Tx) ([]map[string]any, error) {
	return loopreadings.Failing(ctx, tx, ControllerLoops, func(ctx context.Context, tx pgx.Tx, loop string) (loopreadings.Record, error) {
		return loopreadings.Latest(ctx, tx, ControllerStream, lineDigest(loop), loop+passesAgain)
	})
}
