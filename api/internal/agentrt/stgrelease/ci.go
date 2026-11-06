package stgrelease

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// ReleaseCIObservation preserves the latest selection's unsuccessful status
// and identity. Only a complete accepted result becomes ci_accepted material.
// The CI scope and last full green ride along to explain a wait or block.
func ReleaseCIObservation(e landingci.ReleaseEvidence) store.StgCIObservation {
	r, _ := e.Record()
	return store.StgCIObservation{Status: e.Status, Reason: e.Reason, UID: e.UID, Run: e.Run, CreatedAt: e.CreatedAt, ReadAt: e.ReadAt, Accepted: r, Undecided: len(e.Undecided),
		Scope: e.Scope(), Stale: len(e.Stale), LastFullRun: e.LastFullRun, LastFullCommit: e.LastFullCommit, LastFullAt: e.LastFullAt, FullMaxAge: landingci.FullMaxAge}
}
