package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// Importing a recorder log (v5 §5.3 T32 「经导入」, the recorder's half;
// CUT-gate-b-2026-10-19.md §2 item 3 A, Owner 2026-10-19 「都照建议」): the
// infra event recorder's hash-chained log of one work becomes the stream
// legacy:recorder:<work>, so lane.py and rec.py can retire. It takes the
// plan import's shape (planimportcard.go): POST /plan/imports with a request
// of format agentrt.recorder_import/v1; the chain is checked on the lines as
// the recorder wrote them and the local paths masked (recorderimport.Read);
// the import is tried whole and rolled back; the kept log is stored as an
// artifact and the Owner gets one decision card. The proposal is a
// plan_change_proposed of one change, op recorder_import, on the log's
// stream, so the controller's plan decision pass (plandecide.go) decides it
// like a plan import: on Import it lands the log under that decision. A log
// is imported once: the same kept log again answers that it was; another
// log of the same work is refused.

// The plan's decision pass and the progress projection reach the recorder
// import through the plan code's ports (planstore, planports.go).
func init() {
	planstore.BindPlanRecorderImport(planstore.RecorderImportPorts{LogOf: recorderImportOf, Decide: externalwork.DecideRecorderImport})
}

// OpRecorderImport is the change a recorder import proposal holds.
const OpRecorderImport = "recorder_import"

// The recorder import card's options; the decision pass applies the first.
const (
	RecorderApply  = "Import the log"
	RecorderRefuse = "Do not import it"
	// RecorderRecommendWhy is why a recorder import's card recommends
	// importing (the Owner 2026-10-21 「2.3 的四条都照推荐 A」, CUT-round2-2.3-rest
	// §3 第 4 条: STG's card rule answers these cards, PROD's Owner reads
	// it; as a plan change's card recommends applying, TD-397): the trial
	// import has checked the log whole, and the log is history.
	RecorderRecommendWhy = "the trial import checked the hash chain from the first line to the last and kept the log; " +
		"it is history, so no plan or mission waits on it, and once imported it is fixed"
)

// recorderImportOf is the kept log of a proposal that is a recorder import.
func recorderImportOf(changes []planstore.PlanChange) (string, bool) {
	if len(changes) == 1 && changes[0].Op == OpRecorderImport && changes[0].Artifact != "" {
		return changes[0].Artifact, true
	}
	return "", false
}

// ProposeRecorderImport proposes importing a recorder log, why reason says.
// key is the writer's idempotency key. A log imported already answers so,
// with no card; one waiting on the Owner answers with its card.
func ProposeRecorderImport(ctx context.Context, db DB, w Writer, key, reason string, req recorderimport.Request, ask planstore.Ask) (map[string]any, error) {
	switch {
	case key == "":
		return nil, invalid("an idempotency key is required")
	case strings.TrimSpace(reason) == "":
		return nil, invalid("a recorder import says why")
	}
	log, err := recorderimport.Read(req)
	if err != nil {
		return nil, invalid("recorder import: %v", err)
	}
	// What the decision will check, checked now: a log the Owner could
	// answer Import to and that would then fail is refused before any card
	// (review 402 B1).
	if err = recorderimport.Check(log); err != nil {
		return nil, invalid("recorder import: the kept log does not check: %v", err)
	}
	doc, logID, err := externalwork.KeptLog(log)
	if err != nil {
		return nil, err
	}
	work := log.Source.WorkID
	stream := externalwork.RecorderStream(work)
	request := map[string]any{"work_id": work, "log_artifact_id": logID, "reason": reason}
	return idempotentAs(ctx, db, w, "plan/imports", key, request, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if err := lockStream(ctx, tx, stream); err != nil {
			return nil, err
		}
		head, other, err := externalwork.RecorderImported(ctx, tx, stream, logID)
		switch {
		case err != nil:
			return nil, err
		case head != 0:
			return map[string]any{"decision": "applied", "imported": false, "event_id": head, "log_artifact_id": logID}, nil
		case other != "":
			return nil, invalid("the recorder log of %s was already imported from %s; a log is imported once", work, other)
		}
		waiting, err := waitingRecorderImport(ctx, tx, stream)
		if err != nil {
			return nil, err
		}
		if waiting != nil {
			if waiting.Artifact != logID {
				return nil, invalid("another import of %s's log (%s) waits on the Owner's card %s", work, waiting.Artifact, waiting.Question)
			}
			return map[string]any{"proposal_event_id": waiting.Event, "decision": "", "question_id": waiting.Question,
				"log_artifact_id": logID, "waiting": "the Owner decides a recorder import"}, nil
		}
		if err = externalwork.TryRecorderImport(ctx, tx, log, doc, logID); err != nil {
			return nil, err
		}
		if _, err = artifact.PutArtifact(ctx, tx, w.Actor, "other", "application/json", doc); err != nil {
			return nil, err
		}
		cardKey := planstore.CanonKey(w.Actor, "recorder:"+key)
		body := recorderCardBody(w, cardKey, reason, log, logID)
		if n := questions.BodyBytes(body); n > questions.BodyLimit {
			return nil, invalid("the Owner's card for this import would be %d bytes, more than the question store's %d", n, questions.BodyLimit)
		}
		if ask == nil {
			return nil, fmt.Errorf("%w: no question store is configured", planstore.ErrAsk)
		}
		byRule, err := planstore.RuleAnswers(ctx, tx, planstore.CardRecorderImport, "")
		if err != nil {
			return nil, err
		}
		q, err := ask(ctx, planstore.Card{Key: cardKey, Title: "Import recorder log " + work + ": " + firstLine(reason), Body: body,
			Options: []string{RecorderApply, RecorderRefuse}, Recommended: 1, RecommendedWhy: RecorderRecommendWhy,
			Meanwhile: "the log is not imported; the recorder keeps it as it is",
			OwnerDuty: "irreversible", Urgency: "when you can: only this import waits for you", ByRule: byRule})
		if errors.Is(err, ErrInvalid) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", planstore.ErrAsk, err)
		}
		changes, err := planstore.ChangesDoc([]planstore.PlanChange{{Op: OpRecorderImport, Artifact: logID}})
		if err != nil {
			return nil, err
		}
		proposed, err := Append(ctx, tx, Event{Stream: stream, Type: "plan_change_proposed", Actor: w.Actor,
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
func waitingRecorderImport(ctx context.Context, tx pgx.Tx, stream string) (*planstore.PendingImport, error) {
	var p planstore.PendingImport
	err := tx.QueryRow(ctx, `SELECT p.event_id, p.body_json->'changes'->0->>'artifact', p.body_json->>'question_id' FROM agentrt.event p
	  WHERE p.event_stream_id = $1 AND p.event_type = 'plan_change_proposed' AND p.body_json->'changes'->0->>'op' = $2
	    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided' AND d.event_stream_id = p.event_stream_id
	      AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
	  ORDER BY p.event_id LIMIT 1`, stream, OpRecorderImport).Scan(&p.Event, &p.Artifact, &p.Question)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// recorderCardBody is the import card's text: who proposes, which log, what
// it holds, and what each answer does.
func recorderCardBody(w Writer, cardKey, reason string, log recorderimport.Log, logID string) string {
	return recorderimport.CardBody(w.Actor, cardKey, reason, log, logID, externalwork.RecorderStream(log.Source.WorkID))
}

// firstLine is the plan code's helper of the same name (planstore), which
// this file used while it shared its package; copied, 3 lines.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
