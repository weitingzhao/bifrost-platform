// Package loopalert records a controller loop that keeps failing the same
// way, and the note that it passes again, on the controller stream; the reads
// of those records are loopreadings'. Moved from the store 2026-11-17.
package loopalert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
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

// FailingPhrase is the alert's phrase (the event vocabulary lists it).
const FailingPhrase = "a controller loop failed the same way several times in a row"

// ControllerLoops are the controller's loops (controller.Start), the names an
// alert's line_digest is read back to.
var ControllerLoops = []string{"agentrt-leases", "agentrt-facts-rules", "agentrt-card-rule", "agentrt-assess", "agentrt-landings", "agentrt-disk-pages",
	"agentrt-presence", "agentrt-slots"}

// errorKept bounds the error an alert keeps.
const errorKept = 4096

// AlertFailingLoop records that loop failed n times in a row with errText.
func AlertFailingLoop(ctx context.Context, db eventlog.DB, loop string, n int, errText string) error {
	text := loopreadings.FailureText(loop, n, errText, errorKept)
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		art, err := artifact.PutArtifact(ctx, tx, "controller", "log_tail", "text/plain; charset=utf-8", []byte(text))
		if err != nil {
			return err
		}
		_, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: ControllerStream, Type: failingType, Actor: "controller",
			Body: map[string]any{"loop": loop, "failures": n, "artifact_id": art, "line_digest": lineDigest(loop)}})
		return err
	})
}

// NoteLoopPasses records that loop passed again after n failures in a row
// (n 0: the count is not known, as after a restart).
func NoteLoopPasses(ctx context.Context, db eventlog.DB, loop string, n int) error {
	text := loopreadings.RecoveryText(loop, n, passesAgain)
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: ControllerStream, Type: recoveredType, Actor: "controller",
			Body: map[string]any{"loop": loop, "failures": n, "line_digest": lineDigest(loop), "text": text}})
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
func LoopAlertOpen(ctx context.Context, db eventlog.DB, loop string) (bool, error) {
	var open bool
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		r, err := latestLoopRecord(ctx, tx, loop)
		open = r.alert
		return err
	})
	return open, err
}

// FailingLoops lists the loops whose latest record is an alert. The plan
// code's Progress reads them through its port (planstore, planports.go); the
// store's wiring (portsbind.go) binds it to this.
func FailingLoops(ctx context.Context, tx pgx.Tx) ([]map[string]any, error) {
	return loopreadings.Failing(ctx, tx, ControllerLoops, func(ctx context.Context, tx pgx.Tx, loop string) (loopreadings.Record, error) {
		return loopreadings.Latest(ctx, tx, ControllerStream, lineDigest(loop), loop+passesAgain)
	})
}

// lineDigest names an alert line in its event.
func lineDigest(line string) string {
	sum := sha256.Sum256([]byte(line))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The controller loops' own event types (2026-11-17, migration 0029): every
// event type has one owning domain, and these two are the foundation's
// (registered under foundation in bus/registry/ledger/types.yaml). Before,
// an alert was a policy_alert of rule controller_failing and a recovery a
// free-text note; those rows stay and loopreadings still reads them.
const (
	// failingType: loop failed failures times in a row the same way; its
	// error is kept as a log_tail artifact; line_digest names the loop.
	failingType = "controller_loop_failing"
	// recoveredType: loop passes again after failures failures in a row (0:
	// not known, as after a restart); text says so in words.
	recoveredType = "controller_loop_recovered"
)

// loopTypes are this package's event types.
var loopTypes = []string{failingType, recoveredType}

var loopBodies = map[string][]eventlog.Field{
	failingType: {{Name: "loop", Kind: eventlog.Str}, {Name: "failures", Kind: eventlog.Int}, {Name: "artifact_id", Kind: eventlog.ID},
		{Name: "line_digest", Kind: eventlog.Str}},
	recoveredType: {{Name: "loop", Kind: eventlog.Str}, {Name: "failures", Kind: eventlog.Int}, {Name: "line_digest", Kind: eventlog.Str},
		{Name: "text", Kind: eventlog.Str}},
}

var loopValues = map[string]map[string]eventlog.Value{
	failingType:   {"line_digest": digestValue},
	recoveredType: {"line_digest": digestValue},
}

var digestValue = eventlog.Shaped("a line digest (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))

func init() {
	eventlog.Declare(eventlog.Vocabulary{Types: loopTypes, Bodies: loopBodies, Values: loopValues})
}
