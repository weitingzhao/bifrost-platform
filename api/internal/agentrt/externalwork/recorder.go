package externalwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
)

// The recorder import's landing (moved from store/recorderimport.go on
// 2026-11-17, plan core wall step 6): a work's recorder log kept as an
// artifact and written as the stream legacy:recorder:<work>, tried whole in a
// savepoint before the Owner's card and landed under his decision. Proposing
// the import is the plan's (planstore.ProposeRecorderImport: a plan change
// with the Owner's card); this package prepares the log for it
// (PrepareRecorderImport), and the plan's decision pass reaches
// DecideRecorderImport through the port the store's wiring binds
// (planstore.BindPlanRecorderImport).

// recorderStream is the stream a work's recorder log is imported into.
func recorderStream(work string) string { return "legacy:recorder:" + work }

// recorderSource is the head event's source: the kept log it came from.
func recorderSource(logID string) string { return "recorder_import:" + logID }

// KeptLog gives a log's canonical bytes and the artifact id they are stored
// under.
func KeptLog(log recorderimport.Log) ([]byte, string, error) {
	raw, err := json.Marshal(log)
	if err != nil {
		return nil, "", err
	}
	doc, err := canon.Marshal(raw)
	if err != nil {
		return nil, "", err
	}
	if len(doc) > artifact.MaxArtifactBytes {
		return nil, "", eventlog.Invalid("recorder import: the log is %d bytes kept; an artifact is at most %d", len(doc), artifact.MaxArtifactBytes)
	}
	sum := sha256.Sum256(doc)
	return doc, "sha256:" + hex.EncodeToString(sum[:]), nil
}

// RecorderImported is the head of stream's import when it came from logID,
// or the source of the import from another log; both zero when the stream
// has no import.
func RecorderImported(ctx context.Context, tx pgx.Tx, stream, logID string) (int64, string, error) {
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

// TryRecorderImport lands the log in a savepoint with every deferred check
// made at once, then rolls back.
func TryRecorderImport(ctx context.Context, tx pgx.Tx, log recorderimport.Log, doc []byte, logID string) error {
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

// DecideRecorderImport lands a recorder import proposal's log in tx under
// the decision decided: the log is read back from its artifact and checked
// again.
func DecideRecorderImport(ctx context.Context, tx pgx.Tx, artifact string, decided int64) error {
	var doc []byte
	err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, artifact).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventlog.Invalid("the recorder import's log %s is not stored", artifact)
	}
	if err != nil {
		return err
	}
	var log recorderimport.Log
	if err = decodeStrict(doc, &log); err != nil {
		return eventlog.Invalid("the recorder import's log %s does not read: %v", artifact, err)
	}
	if err = recorderimport.Check(log); err != nil {
		return eventlog.Invalid("the recorder import's log %s does not check: %v", artifact, err)
	}
	again, logID, err := KeptLog(log)
	if err != nil {
		return err
	}
	if logID != artifact || string(again) != string(doc) {
		return eventlog.Invalid("the recorder import's log %s does not read back as itself", artifact)
	}
	_, err = landRecorder(ctx, tx, "controller", log, doc, logID, decided)
	return err
}

// landRecorder writes the log as its work's stream, the head caused by cause
// (the decision; 0 for none) and each record caused by the head, or finds
// that this same log was imported already. It returns the head.
func landRecorder(ctx context.Context, tx pgx.Tx, actor string, log recorderimport.Log, doc []byte, logID string, cause int64) (int64, error) {
	stream := recorderStream(log.Source.WorkID)
	if err := eventlog.LockStream(ctx, tx, stream); err != nil {
		return 0, err
	}
	head, other, err := RecorderImported(ctx, tx, stream, logID)
	switch {
	case err != nil:
		return 0, err
	case head != 0:
		return head, nil
	case other != "":
		return 0, eventlog.Invalid("the recorder log of %s was already imported from %s; a log is imported once", log.Source.WorkID, other)
	}
	if _, err = artifact.PutArtifact(ctx, tx, actor, "other", "application/json", doc); err != nil {
		return 0, err
	}
	h, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: "legacy_imported", Actor: actor, Cause: cause,
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
		if _, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: "legacy_imported", Actor: actor, Cause: h.EventID, Body: body}); err != nil {
			return 0, fmt.Errorf("recorder import: record %d: %w", r.Seq, err)
		}
	}
	return h.EventID, nil
}

// decodeStrict is the plan code's helper of the same name (planstore),
// copied, 10 lines.
func decodeStrict(doc []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("more than one value")
	}
	return nil
}

// PreparedRecorderLog is a recorder log read, checked and kept, as the plan's
// proposal of its import needs it (planstore.RecorderLog has the same
// fields): the work, the stream it would be imported into, the kept log's
// artifact id and bytes, the Owner's card text, and in tx whether the log was
// imported already and the whole import tried and rolled back.
type PreparedRecorderLog struct {
	Work, Stream, LogID string
	Doc                 []byte
	CardBody            func(actor, cardKey, reason string) string
	Imported            func(ctx context.Context, tx pgx.Tx) (head int64, other string, err error)
	TryImport           func(ctx context.Context, tx pgx.Tx) error
}

// PrepareRecorderImport reads req's log and checks what the decision will
// check, before any card: a log the Owner could answer Import to and that
// would then fail is refused now (review 402 B1). eventlog.ErrInvalid for a
// log that does not read or check. (Moved from store/recorderimport.go,
// 2026-11-17: the lineage's half of proposing the import.)
func PrepareRecorderImport(req recorderimport.Request) (PreparedRecorderLog, error) {
	log, err := recorderimport.Read(req)
	if err != nil {
		return PreparedRecorderLog{}, fmt.Errorf("%w: recorder import: %v", eventlog.ErrInvalid, err)
	}
	if err = recorderimport.Check(log); err != nil {
		return PreparedRecorderLog{}, fmt.Errorf("%w: recorder import: the kept log does not check: %v", eventlog.ErrInvalid, err)
	}
	doc, logID, err := KeptLog(log)
	if err != nil {
		return PreparedRecorderLog{}, err
	}
	work := log.Source.WorkID
	stream := recorderStream(work)
	return PreparedRecorderLog{Work: work, Stream: stream, LogID: logID, Doc: doc,
		CardBody: func(actor, cardKey, reason string) string {
			return recorderimport.CardBody(actor, cardKey, reason, log, logID, stream)
		},
		Imported: func(ctx context.Context, tx pgx.Tx) (int64, string, error) {
			return RecorderImported(ctx, tx, stream, logID)
		},
		TryImport: func(ctx context.Context, tx pgx.Tx) error {
			return TryRecorderImport(ctx, tx, log, doc, logID)
		},
	}, nil
}
