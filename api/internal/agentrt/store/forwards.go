package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The names below are other domains' (coord, artifact, actor). They stay
// here, as one-line forwards, for two kinds of caller that this package's
// move of coord, artifact and seed code (decoupling item 5, S2, 2026-11-18)
// did not touch: the approvals tests (a trust anchor, internal/approvals/**,
// reach them as runtime.X) and the agentrt API and migrate command that
// start through them. Each goes when its callers go direct.

func Seed(ctx context.Context, conn interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}) (bool, error) {
	return actor.Seed(ctx, conn)
}

func Unread(ctx context.Context, db eventlog.DB, w actor.Writer, limit int) ([]coord.Message, error) {
	return coord.Unread(ctx, db, w, limit)
}

func AckMessages(ctx context.Context, db eventlog.DB, w actor.Writer, ids []string) ([]string, error) {
	return coord.AckMessages(ctx, db, w, ids)
}

func Thread(ctx context.Context, db eventlog.DB, actor, with string, limit int) ([]coord.Message, error) {
	return coord.Thread(ctx, db, actor, with, limit)
}

func PostConclusion(ctx context.Context, db eventlog.DB, key, to, body string) error {
	return coord.PostConclusion(ctx, db, key, to, body)
}

func UploadArtifact(ctx context.Context, db eventlog.DB, w actor.Writer, kind, mediaType string, body []byte) (string, bool, error) {
	return artifact.UploadArtifact(ctx, db, w, kind, mediaType, body)
}
