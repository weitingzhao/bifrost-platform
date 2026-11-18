package store

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The artifact store (bodies, uploads, their search and scans) is package
// artifact. It imports the bases and not this package; the check of the
// artifacts that are plan views or external work descriptions is this
// package's and reaches it by registration (planview.go).

// UploadArtifact is artifact.UploadArtifact. The name stays here for the
// approvals' tests, which register a thread through this package.
func UploadArtifact(ctx context.Context, db eventlog.DB, w actor.Writer, kind, mediaType string, body []byte) (string, bool, error) {
	return artifact.UploadArtifact(ctx, db, w, kind, mediaType, body)
}
