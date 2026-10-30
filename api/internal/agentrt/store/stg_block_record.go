package store

import (
	"maps"
	"time"
)

// StgBlockedRecord is the shared constructor for the controller and final
// gate. Unknown baselines have one closed-protocol representation; preserve
// the original refusal in detail so it can be re-evaluated after recovery.
// The returned record has passed the same parser used by Append and replay.
func StgBlockedRecord(p, why string, base StgRecord, at time.Time, version string, detail map[string]any) (StgRecord, error) {
	detail = maps.Clone(detail)
	if detail == nil {
		detail = map[string]any{}
	}
	if why == "baseline_unknown" || !stgBase(map[string]any(base)) {
		if why != "baseline_unknown" {
			detail["refusal"] = why
		}
		why, base = "baseline_unknown", StgRecord{"P": ""}
	}
	r := StgRecord{"v": "1", "kind": "blocked", "scope": "block", "block": StgBlockID(p, base.String("P"), why, version),
		"why": why, "P": p, "base": map[string]any(base), "first_at": at.Format(time.RFC3339Nano), "evidence_version": version, "detail": detail}
	return ParseStgRelease(r)
}
