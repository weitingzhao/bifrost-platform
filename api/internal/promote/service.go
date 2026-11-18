package promote

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// OverlayContext merges persisted gate state into spine context for GET /context.
// Safe for concurrent callers: never mutates maps owned by base.
func OverlayContext(base *opscontext.File, store *Store) *opscontext.File {
	if base == nil || store == nil {
		return base
	}
	rec, err := store.LoadTier(GateTierProd)
	if err != nil || rec == nil {
		return base
	}
	out := *base
	// last_gate is stale release-gate state. /context no longer publishes it (LANE-W32).
	out.Promotion.LastGate = opscontext.LastGate{}
	// Shallow-copy File would share EnvironmentsExtended with base — concurrent
	// GET /context writes caused "fatal error: concurrent map writes".
	ext := make(map[string]opscontext.EnvironmentExtended, len(base.EnvironmentsExtended)+1)
	for k, v := range base.EnvironmentsExtended {
		ext[k] = v
	}
	out.EnvironmentsExtended = ext
	stg := out.EnvironmentsExtended["staging"]
	stgSmokeOK := false
	for _, c := range rec.Checks {
		if c.ID == "stg-api-monitor" && (c.Reachability == probe.ReachOK || c.Reachability == probe.ReachDegraded) {
			stgSmokeOK = true
		}
	}
	stgRec, _ := store.LoadTier(GateTierStg)
	if stgRec != nil && stgRec.Result == "pass" {
		stg.Status = "IN_PROGRESS"
		stg.Note = "STG release gate pass — Tier B + Promote prod track separate"
	} else if stgSmokeOK {
		stg.Status = "IN_PROGRESS"
		stg.Note = "K3s bifrost-stg smoke OK — run STG release gate on Promote"
	} else if rec.Result == "pass" {
		stg.Status = "IN_PROGRESS"
		stg.Note = "Prod release gate pass; verify stg on Delivery"
	}
	out.EnvironmentsExtended["staging"] = stg
	return &out
}
