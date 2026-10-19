package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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

// OpRecorderImport is the change a recorder import proposal holds.
const OpRecorderImport = "recorder_import"

// The recorder import card's options; the decision pass applies the first.
const (
	RecorderApply  = "Import the log"
	RecorderRefuse = "Do not import it"
)

// RecorderStream is the stream a work's recorder log is imported into.
func RecorderStream(work string) string { return "legacy:recorder:" + work }

// recorderSource is the head event's source: the kept log it came from.
func recorderSource(logID string) string { return "recorder_import:" + logID }

// recorderImportOf is the kept log of a proposal that is a recorder import.
func recorderImportOf(changes []PlanChange) (string, bool) {
	if len(changes) == 1 && changes[0].Op == OpRecorderImport && changes[0].Artifact != "" {
		return changes[0].Artifact, true
	}
	return "", false
}

// keptLog gives a log's canonical bytes and the artifact id they are stored
// under.
func keptLog(log recorderimport.Log) ([]byte, string, error) {
	raw, err := json.Marshal(log)
	if err != nil {
		return nil, "", err
	}
	doc, err := canon.Marshal(raw)
	if err != nil {
		return nil, "", err
	}
	if len(doc) > maxArtifactBytes {
		return nil, "", invalid("recorder import: the log is %d bytes kept; an artifact is at most %d", len(doc), maxArtifactBytes)
	}
	sum := sha256.Sum256(doc)
	return doc, "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ProposeRecorderImport proposes importing a recorder log, why reason says.
// key is the writer's idempotency key. A log imported already answers so,
// with no card; one waiting on the Owner answers with its card.
func ProposeRecorderImport(ctx context.Context, db DB, w Writer, key, reason string, req recorderimport.Request, ask Ask) (map[string]any, error) {
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
	doc, logID, err := keptLog(log)
	if err != nil {
		return nil, err
	}
	work := log.Source.WorkID
	stream := RecorderStream(work)
	request := map[string]any{"work_id": work, "log_artifact_id": logID, "reason": reason}
	return idempotentAs(ctx, db, w, "plan/imports", key, request, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if err := lockStream(ctx, tx, stream); err != nil {
			return nil, err
		}
		head, other, err := recorderImported(ctx, tx, stream, logID)
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
			if waiting.artifact != logID {
				return nil, invalid("another import of %s's log (%s) waits on the Owner's card %s", work, waiting.artifact, waiting.question)
			}
			return map[string]any{"proposal_event_id": waiting.event, "decision": "", "question_id": waiting.question,
				"log_artifact_id": logID, "waiting": "the Owner decides a recorder import"}, nil
		}
		if err = tryRecorderImport(ctx, tx, log, doc, logID); err != nil {
			return nil, err
		}
		if _, err = PutArtifact(ctx, tx, w.Actor, "other", "application/json", doc); err != nil {
			return nil, err
		}
		cardKey := canonKey(w.Actor, "recorder:"+key)
		body := recorderCardBody(w, cardKey, reason, log, logID)
		if n := questions.BodyBytes(body); n > questions.BodyLimit {
			return nil, invalid("the Owner's card for this import would be %d bytes, more than the question store's %d", n, questions.BodyLimit)
		}
		if ask == nil {
			return nil, fmt.Errorf("%w: no question store is configured", ErrAsk)
		}
		q, err := ask(ctx, Card{Key: cardKey, Title: "Import recorder log " + work + ": " + firstLine(reason), Body: body,
			Options: []string{RecorderApply, RecorderRefuse}, Meanwhile: "the log is not imported; the recorder keeps it as it is",
			OwnerDuty: "irreversible", Urgency: "when you can: only this import waits for you"})
		if errors.Is(err, ErrInvalid) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAsk, err)
		}
		changes, err := changesDoc([]PlanChange{{Op: OpRecorderImport, Artifact: logID}})
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

// recorderImported is the head of stream's import when it came from logID,
// or the source of the import from another log; both zero when the stream
// has no import.
func recorderImported(ctx context.Context, tx pgx.Tx, stream, logID string) (int64, string, error) {
	var id int64
	var source string
	err := tx.QueryRow(ctx, `SELECT event_id, body_json->>'source' FROM agentrt.event WHERE event_stream_id = $1
	  AND event_type = 'legacy_imported' AND body_json->>'source' LIKE 'recorder_import:%' ORDER BY event_id LIMIT 1`, stream).Scan(&id, &source)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, "", nil
	case err != nil:
		return 0, "", err
	case source == recorderSource(logID):
		return id, "", nil
	}
	return 0, strings.TrimPrefix(source, "recorder_import:"), nil
}

// waitingRecorderImport is the recorder import of stream that waits on a
// decision, if any.
func waitingRecorderImport(ctx context.Context, tx pgx.Tx, stream string) (*pendingImport, error) {
	var p pendingImport
	err := tx.QueryRow(ctx, `SELECT p.event_id, p.body_json->'changes'->0->>'artifact', p.body_json->>'question_id' FROM agentrt.event p
	  WHERE p.event_stream_id = $1 AND p.event_type = 'plan_change_proposed' AND p.body_json->'changes'->0->>'op' = $2
	    AND NOT EXISTS (SELECT 1 FROM agentrt.event d WHERE d.event_type = 'plan_change_decided' AND d.event_stream_id = p.event_stream_id
	      AND (d.body_json->>'proposal_event_id')::bigint = p.event_id)
	  ORDER BY p.event_id LIMIT 1`, stream, OpRecorderImport).Scan(&p.event, &p.artifact, &p.question)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// tryRecorderImport lands the log in a savepoint with every deferred check
// made at once, then rolls back.
func tryRecorderImport(ctx context.Context, tx pgx.Tx, log recorderimport.Log, doc []byte, logID string) error {
	if _, err := tx.Exec(ctx, `SAVEPOINT try_recorder_import`); err != nil {
		return err
	}
	_, err := landRecorder(ctx, tx, "controller", log, doc, logID, 0)
	if err == nil {
		_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	}
	if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT try_recorder_import`); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

// recorderCardBody is the import card's text: who proposes, which log, what
// it holds, and what each answer does.
func recorderCardBody(w Writer, cardKey, reason string, log recorderimport.Log, logID string) string {
	types := log.Types()
	names := make([]string, 0, len(types))
	for t := range types {
		names = append(names, t)
	}
	slices.Sort(names)
	var counts []string
	for _, t := range names {
		counts = append(counts, fmt.Sprintf("%d %s", types[t], t))
	}
	s := log.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import the recorder log of %s, proposal %s.\n\nWhy: %s\n\n", w.Actor, s.WorkID, cardKey[:12], reason)
	fmt.Fprintf(&b, "%d events: %s. The hash chain holds from the first line to the last.", len(log.Records), strings.Join(counts, ", "))
	if n := log.Masked(); n > 0 {
		fmt.Fprintf(&b, " %d of them had a path into one machine, masked before they are kept; each keeps the digest of its original line.", n)
	}
	fmt.Fprintf(&b, "\n\nFrom %s, sha256 %s; kept log %s.\n\n", s.File, s.SHA256, logID)
	b.WriteString("Importing writes the log as the stream " + RecorderStream(s.WorkID) + ", one event per line, under your decision; " +
		"it is then fixed. Not importing writes nothing; the recorder keeps its log as it is.")
	return b.String()
}

// decideRecorderImport lands a recorder import proposal's log in tx under
// the decision decided: the log is read back from its artifact and checked
// again.
func decideRecorderImport(ctx context.Context, tx pgx.Tx, artifact string, decided int64) error {
	var doc []byte
	err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, artifact).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid("the recorder import's log %s is not stored", artifact)
	}
	if err != nil {
		return err
	}
	var log recorderimport.Log
	if err = decodeStrict(doc, &log); err != nil {
		return invalid("the recorder import's log %s does not read: %v", artifact, err)
	}
	if err = recorderimport.Check(log); err != nil {
		return invalid("the recorder import's log %s does not check: %v", artifact, err)
	}
	again, logID, err := keptLog(log)
	if err != nil {
		return err
	}
	if logID != artifact || string(again) != string(doc) {
		return invalid("the recorder import's log %s does not read back as itself", artifact)
	}
	_, err = landRecorder(ctx, tx, "controller", log, doc, logID, decided)
	return err
}

// landRecorder writes the log as its work's stream, the head caused by cause
// (the decision; 0 for none) and each record caused by the head, or finds
// that this same log was imported already. It returns the head.
func landRecorder(ctx context.Context, tx pgx.Tx, actor string, log recorderimport.Log, doc []byte, logID string, cause int64) (int64, error) {
	stream := RecorderStream(log.Source.WorkID)
	if err := lockStream(ctx, tx, stream); err != nil {
		return 0, err
	}
	head, other, err := recorderImported(ctx, tx, stream, logID)
	switch {
	case err != nil:
		return 0, err
	case head != 0:
		return head, nil
	case other != "":
		return 0, invalid("the recorder log of %s was already imported from %s; a log is imported once", log.Source.WorkID, other)
	}
	if _, err = PutArtifact(ctx, tx, actor, "other", "application/json", doc); err != nil {
		return 0, err
	}
	h, err := Append(ctx, tx, Event{Stream: stream, Type: "legacy_imported", Actor: actor, Cause: cause,
		Body: map[string]any{"source": recorderSource(logID), "count": len(log.Records), "work_id": log.Source.WorkID}})
	if err != nil {
		return 0, err
	}
	for _, r := range log.Records {
		body := map[string]any{"source": "recorder", "work_id": log.Source.WorkID, "seq": r.Seq, "digest": "sha256:" + r.Digest,
			"record": r.Body}
		if r.Masked {
			body["masked"] = true
		}
		if _, err = Append(ctx, tx, Event{Stream: stream, Type: "legacy_imported", Actor: actor, Cause: h.EventID, Body: body}); err != nil {
			return 0, fmt.Errorf("recorder import: record %d: %w", r.Seq, err)
		}
	}
	return h.EventID, nil
}
