package stgrelease

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// ReleaseCIObservation preserves the latest selection's unsuccessful status
// and identity. Only a complete accepted result becomes ci_accepted material.
func ReleaseCIObservation(e landingci.ReleaseEvidence) store.StgCIObservation {
	r, _ := e.Record()
	return store.StgCIObservation{Status: e.Status, Reason: e.Reason, UID: e.UID, CreatedAt: e.CreatedAt, ReadAt: e.ReadAt, Accepted: r}
}
