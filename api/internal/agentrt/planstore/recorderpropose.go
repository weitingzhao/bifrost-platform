package planstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Proposing a recorder import (v5 §5.3 T32 「经导入」, the recorder's half;
// CUT-gate-b-2026-10-19.md §2 item 3 A, Owner 2026-10-19 「都照建议」): the
// infra event recorder's hash-chained log of one work becomes the stream
// legacy:recorder:<work>, so lane.py and rec.py can retire. It takes the plan
// import's shape (planimportcard.go): POST /plan/imports with a request of
// format agentrt.recorder_import/v1; the lineage domain reads and checks the
// log and keeps it (externalwork.PrepareRecorderImport, handed here as a
// RecorderLog); the import is tried whole and rolled back; the kept log is
// stored as an artifact and the Owner gets one decision card. The proposal is
// a plan_change_proposed of one change, op recorder_import, on the log's
// stream, so the decision pass (plandecide.go) decides it like a plan import:
// on Import it lands the log under that decision (DecideRecorderImportLog,
// through the port). A log is imported once: the same kept log again answers
// that it was; another log of the same work is refused.
//
// Moved from store/recorderimport.go on 2026-11-17: it is a plan change, and
// it named the plan's types from outside the plan.

// opRecorderImport is the change a recorder import proposal holds.
const opRecorderImport = "recorder_import"

// RecorderRecommendWhy is why a recorder import's card recommends importing
// (the Owner 2026-10-21 「2.3 的四条都照推荐 A」, CUT-round2-2.3-rest §3 第 4 条:
// STG's card rule answers these cards, PROD's Owner reads it; as a plan
// change's card recommends applying, TD-397): the trial import has checked the
// log whole, and the log is history.
const RecorderRecommendWhy = "the trial import checked the hash chain from the first line to the last and kept the log; " +
	"it is history, so no plan or mission waits on it, and once imported it is fixed"

// RecorderLog is a recorder log the lineage domain has read, checked and
// kept (externalwork.PreparedRecorderLog has the same fields): the work, the
// stream it would be imported into, the kept log's artifact id and bytes, the
// card's text, and in tx whether it was imported already (the head event, or
// the other log of the same work that was) and the whole import tried.
type RecorderLog struct {
	Work, Stream, LogID string
	Doc                 []byte
	CardBody            func(actor, cardKey, reason string) string
	Imported            func(ctx context.Context, tx pgx.Tx) (head int64, other string, err error)
	TryImport           func(ctx context.Context, tx pgx.Tx) error
}

// recorderImportOf is the kept log of a proposal that is a recorder import.
func recorderImportOf(changes []PlanChange) (string, bool) {
	if len(changes) == 1 && changes[0].Op == opRecorderImport && changes[0].Artifact != "" {
		return changes[0].Artifact, true
	}
	return "", false
}

// ProposeRecorderImport proposes importing a recorder log, why reason says.
// key is the writer's idempotency key. A log imported already answers so,
// with no card; one waiting on the Owner answers with its card.
func ProposeRecorderImport(ctx context.Context, db eventlog.DB, w actor.Writer, key, reason string, log RecorderLog, ask Ask) (map[string]any, error) {
	switch {
	case key == "":
		return nil, eventlog.Invalid("an idempotency key is required")
	case strings.TrimSpace(reason) == "":
		return nil, eventlog.Invalid("a recorder import says why")
	case log.CardBody == nil || log.Imported == nil || log.TryImport == nil:
		return nil, eventlog.Invalid("recorder import: the log was not prepared")
	}
	work, stream, logID := log.Work, log.Stream, log.LogID
	request := map[string]any{"work_id": work, "log_artifact_id": logID, "reason": reason}
	return actor.IdempotentAs(ctx, db, w, "plan/imports", key, request, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := actor.Ensure(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if err := eventlog.LockStream(ctx, tx, stream); err != nil {
			return nil, err
		}
		head, other, err := log.Imported(ctx, tx)
		switch {
		case err != nil:
			return nil, err
		case head != 0:
			return map[string]any{"decision": "applied", "imported": false, "event_id": head, "log_artifact_id": logID}, nil
		case other != "":
			return nil, eventlog.Invalid("the recorder log of %s was already imported from %s; a log is imported once", work, other)
		}
		waiting, err := waitingRecorderImport(ctx, tx, stream)
		if err != nil {
			return nil, err
		}
		if waiting != nil {
			if waiting.Artifact != logID {
				return nil, eventlog.Invalid("another import of %s's log (%s) waits on the Owner's card %s", work, waiting.Artifact, waiting.Question)
			}
			return map[string]any{"proposal_event_id": waiting.Event, "decision": "", "question_id": waiting.Question,
				"log_artifact_id": logID, "waiting": "the Owner decides a recorder import"}, nil
		}
		if err = log.TryImport(ctx, tx); err != nil {
			return nil, err
		}
		if _, err = artifact.PutArtifact(ctx, tx, w.Actor, "other", "application/json", log.Doc); err != nil {
			return nil, err
		}
		cardKey := CanonKey(w.Actor, "recorder:"+key)
		body := log.CardBody(w.Actor, cardKey, reason)
		n, limit, err := cardBodyFits(body)
		if err != nil {
			return nil, err
		}
		if n > limit {
			return nil, eventlog.Invalid("the Owner's card for this import would be %d bytes, more than the question store's %d", n, limit)
		}
		if ask == nil {
			return nil, fmt.Errorf("%w: no question store is configured", ErrAsk)
		}
		byRule, err := RuleAnswers(ctx, tx, CardRecorderImport, "")
		if err != nil {
			return nil, err
		}
		q, err := ask(ctx, Card{Key: cardKey, Title: "Import recorder log " + work + ": " + firstLine(reason), Body: body,
			Options: []string{RecorderApplyLabel, RecorderRefuseLabel}, Recommended: 1, RecommendedWhy: RecorderRecommendWhy,
			Meanwhile: "the log is not imported; the recorder keeps it as it is",
			OwnerDuty: "irreversible", Urgency: "when you can: only this import waits for you", ByRule: byRule})
		if errors.Is(err, eventlog.ErrInvalid) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAsk, err)
		}
		changes, err := ChangesDoc([]PlanChange{{Op: opRecorderImport, Artifact: logID}})
		if err != nil {
			return nil, err
		}
		proposed, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: "plan_change_proposed", Actor: w.Actor,
			Body: map[string]any{"changes": changes, "reason": reason, "standing": false, "token": w.Token, "question_id": q}})
		if err != nil {
			return nil, err
		}
		return map[string]any{"proposal_event_id": proposed.EventID, "decision": "", "question_id": q, "log_artifact_id": logID,
			"waiting": "the Owner decides a recorder import"}, nil
	})
}

// waitingRecorderImport is the recorder import of stream that waits on a
// decision, if any.
func waitingRecorderImport(ctx context.Context, tx pgx.Tx, stream string) (*PendingImport, error) {
	var p PendingImport
	err := tx.QueryRow(ctx, `SELECT p.event_id, p.body_json->'changes'->0->>'artifact', p.body_json->>'question_id' FROM agentrt.event p
	  WHERE p.event_stream_id = $1 AND p.event_type = 'plan_change_proposed' AND p.body_json->'changes'->0->>'op' = $2
	    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided' AND d.event_stream_id = p.event_stream_id
	      AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
	  ORDER BY p.event_id LIMIT 1`, stream, opRecorderImport).Scan(&p.Event, &p.Artifact, &p.Question)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}
