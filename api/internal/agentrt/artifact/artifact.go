package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// MaxArtifactBytes is the largest artifact body.
const MaxArtifactBytes = 1 << 20

// PutArtifact stores a body by content and returns its id. Artifacts are
// append only; the same body stored twice is one row.
func PutArtifact(ctx context.Context, tx pgx.Tx, actor, kind, mediaType string, body []byte) (string, error) {
	if err := checkBody(kind, body); err != nil {
		return "", err
	}
	if len(body) > MaxArtifactBytes {
		return "", errors.New("store: an artifact is at most 1 MiB")
	}
	h := sha256.Sum256(body)
	id := "sha256:" + hex.EncodeToString(h[:])
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.artifact (artifact_id, artifact_kind, media_type, size_bytes, created_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5) ON CONFLICT (artifact_id) DO NOTHING`, id, kind, mediaType, len(body), actor); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.artifact_blob (artifact_id, body) VALUES ($1,$2) ON CONFLICT (artifact_id) DO NOTHING`,
		id, body); err != nil {
		return "", err
	}
	return id, nil
}

// ArtifactStream is the event stream of uploaded artifacts.
const ArtifactStream = "artifacts"

// UploadArtifact stores an uploaded body (PUT /artifacts, v5 §5.2) and, the
// first time this body is stored, records artifact_put in the same
// transaction: the writer it is recorded as (a thread registered on its
// first write, as for proposals; review 175 B4), the token that uploaded it,
// its kind and size (review 156 A3). Uploading the same body again stores
// and records nothing. Artifacts that other events create and name (a task
// spec with task_proposed, a message with message_posted) go through
// PutArtifact.
func UploadArtifact(ctx context.Context, db eventlog.DB, w actor.Writer, kind, mediaType string, body []byte) (string, bool, error) {
	if err := checkBody(kind, body); err != nil {
		return "", false, err
	}
	if len(body) > MaxArtifactBytes {
		return "", false, errors.New("store: an artifact is at most 1 MiB")
	}
	h := sha256.Sum256(body)
	id := "sha256:" + hex.EncodeToString(h[:])
	who, uploadedBy := w.Actor, w.Token
	var created bool
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var have bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.artifact WHERE artifact_id = $1)`, id).Scan(&have); err != nil {
			return err
		}
		if have {
			// Nothing to store: the writer is still checked, without
			// registering a thread for it (review 176 B3).
			_, err := actor.CheckWriter(ctx, tx, w)
			return err
		}
		if err := actor.Ensure(ctx, tx, w, true); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO agentrt.artifact (artifact_id, artifact_kind, media_type, size_bytes, created_by_actor_id)
		  VALUES ($1,$2,$3,$4,$5) ON CONFLICT (artifact_id) DO NOTHING`, id, kind, mediaType, len(body), who)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		created = true
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.artifact_blob (artifact_id, body) VALUES ($1,$2)`, id, body); err != nil {
			return err
		}
		_, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: ArtifactStream, Type: "artifact_put", Actor: who,
			Body: map[string]any{"artifact_id": id, "artifact_kind": kind, "size_bytes": len(body), "uploaded_by": uploadedBy}})
		return err
	})
	return id, created, err
}

func HaveArtifacts(ctx context.Context, tx pgx.Tx, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var missing []string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id ORDER BY id), '{}') FROM unnest($1::text[]) AS id
	  WHERE NOT EXISTS (SELECT 1 FROM agentrt.artifact WHERE artifact_id = id)`, ids).Scan(&missing); err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: artifacts not stored: %v", eventlog.ErrState, missing)
	}
	return nil
}
