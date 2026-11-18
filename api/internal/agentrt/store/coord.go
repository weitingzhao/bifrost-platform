package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

// The coordination domain (messages, controls, seats, the record of a text
// that claims an approval) is package coord. It imports the bases and not
// this package. What stays here is the two things it takes by registration
// at start, and the four names the approvals tests still call: where a
// message's body is stored (artifact.PutArtifact) and the record of a claim
// (coord.AlertClaim, its own since claims.go moved there on 2026-11-17; the
// registration stays until coord calls both directly).

func init() { coord.Register(artifact.PutArtifact, coord.AlertClaim) }

func Unread(ctx context.Context, db DB, w Writer, limit int) ([]coord.Message, error) {
	return coord.Unread(ctx, db, w, limit)
}

func AckMessages(ctx context.Context, db DB, w Writer, ids []string) ([]string, error) {
	return coord.AckMessages(ctx, db, w, ids)
}

func Thread(ctx context.Context, db DB, actor, with string, limit int) ([]coord.Message, error) {
	return coord.Thread(ctx, db, actor, with, limit)
}

// PostConclusion is coord.PostConclusion. The name stays here for the
// approvals' and questions' tests, which post through this package.
func PostConclusion(ctx context.Context, db DB, key, to, body string) error {
	return coord.PostConclusion(ctx, db, key, to, body)
}

// AckMessage and SetControl are coord's; only tests call them through here
// (TD-582).
func AckMessage(ctx context.Context, tx pgx.Tx, actor, messageID string) error {
	return coord.AckMessage(ctx, tx, actor, messageID)
}

func SetControl(ctx context.Context, tx pgx.Tx, actor, controlID, scope, scopeRef, kind, reason string) error {
	return coord.SetControl(ctx, tx, actor, controlID, scope, scopeRef, kind, reason)
}
