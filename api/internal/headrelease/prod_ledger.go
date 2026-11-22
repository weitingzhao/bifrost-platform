package headrelease

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The ledger record of each PROD judgement (migration 0043, 2026-11-21).
//
// Why: the loop writes a judgement when it changes (would it release PROD
// now, which of ProdConditions fail and why, what it released). Until now
// that went to the workers' log and to their own audit file; the ledger's
// audit.action.recorded event kept the outcome ("wait") and not the
// reasons, so where a release was held could not be read back by anyone
// but a reader of that pod's log, which is gone with the pod.

// ProdJudgedType is the event type of a judgement.
const ProdJudgedType = "prod_release_judged"

// ProdLedgerStream is the stream the judgements are appended to.
const ProdLedgerStream = "head-release"

// prodLedgerActor writes them: the loop runs in the controller's process, and
// an event's actor must be a registered one (agentrt.actor; the shadow's
// principal names the audit entries, it is not an actor).
const prodLedgerActor = "controller"

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types: []string{ProdJudgedType},
		Bodies: map[string][]eventlog.Field{
			ProdJudgedType: {{Name: "result", Kind: eventlog.Str}, {Name: "record", Kind: eventlog.Any}},
		},
	})
}

// NoteProdJudged appends one judgement: result (would_release, wait,
// unknown, released, error) and the record the audit entry carries as its
// detail (prodRecord, JSON). A detail that is not a JSON object is kept as
// its text, so nothing the loop says is dropped.
func NoteProdJudged(ctx context.Context, db eventlog.DB, result, detail string) error {
	var record any
	var obj map[string]any
	if err := json.Unmarshal([]byte(detail), &obj); err == nil && obj != nil {
		record = obj
	} else {
		record = map[string]any{"text": detail}
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: ProdLedgerStream, Type: ProdJudgedType, Actor: prodLedgerActor,
			Body: map[string]any{"result": result, "record": record}})
		return err
	})
}

// ProdJudgement is one judgement read back, newest first.
type ProdJudgement struct {
	At     time.Time       `json:"at"`
	Result string          `json:"result"`
	Record json.RawMessage `json:"record"`
}

// ProdJudgements reads the judgements written since, newest first, at most
// limit of them.
func ProdJudgements(ctx context.Context, db eventlog.DB, since time.Time, limit int) ([]ProdJudgement, error) {
	var rows []eventlog.Row
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		rows, err = eventlog.OfType(ctx, tx, ProdJudgedType, since, eventlog.Now(ctx).Add(time.Minute))
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]ProdJudgement, 0, limit)
	for i := len(rows) - 1; i >= 0 && len(out) < limit; i-- {
		r := rows[i]
		if r.Stream != ProdLedgerStream {
			continue
		}
		result, _ := r.Body["result"].(string)
		record, err := json.Marshal(r.Body["record"])
		if err != nil {
			return nil, err
		}
		out = append(out, ProdJudgement{At: r.At, Result: result, Record: record})
	}
	return out, nil
}

// RecordProdJudged is NoteProdJudged for the loop's audit hook: bounded to
// five seconds, a failure logged and nothing else (the loop does not depend
// on the ledger). A nil db writes nothing.
func RecordProdJudged(db eventlog.DB, result, detail string) {
	if db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := NoteProdJudged(ctx, db, result, detail); err != nil {
		slog.Warn("prod batch shadow: judgement not written to the ledger", "result", result, "err", err)
	}
}
