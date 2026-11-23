package headrelease

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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
	// canon.Parse keeps numbers as json.Number, which the ledger's canonical
	// form takes as integers (2026-11-22: json.Unmarshal made "commits": 15 a
	// float64 and every judgement with a number in it failed to be written).
	// A detail that is not a JSON object, or holds a number that is not an
	// integer, is kept as its text.
	record := any(map[string]any{"text": detail})
	if v, err := canon.Parse([]byte(detail)); err == nil {
		if obj, ok := v.(map[string]any); ok {
			if _, err := canon.Encode(obj); err == nil {
				record = obj
			}
		}
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

// ProdJudgementsRoute is GET /api/v1/delivery/prod-judgements?limit=…: the
// newest PROD judgements in the ledger (at most limit, default 20, max 200),
// newest first, of the last 7 days. 2026-11-22: the reasons the loop holds a
// PROD release were in the ledger but nothing showed them; this is the read
// the Console and agents use. Read-only; viewer and above.
type ProdJudgementsRoute struct {
	// DB is the agent runtime's database, nil when it is not connected.
	DB func() eventlog.DB
}

func (rt ProdJudgementsRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeRouteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is a whole number from 1 to 200"})
			return
		}
		limit = n
	}
	db := rt.DB()
	if db == nil {
		writeRouteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the agent runtime's database is not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	got, err := ProdJudgements(ctx, db, time.Now().Add(-7*24*time.Hour), limit)
	if err != nil {
		writeRouteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeRouteJSON(w, http.StatusOK, map[string]any{"mode": ProdShadowMode(), "judgements": got})
}
