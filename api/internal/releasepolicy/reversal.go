package releasepolicy

// Reversal says how a release is undone and what undoing it leaves behind.
// It is information: it changes no verdict. Origin: 2026-11-23, LAW-6 (the
// Owner, relayed by Foundation: before a release it must be known that it
// can be taken back); the first step counts how many releases carry
// something a rollback does not take back, before anything is refused for it.
type Reversal struct {
	// Ways are the ways this release is undone, the usual one first.
	Ways []string `json:"ways"`
	// Stays names what a rollback leaves in place: each migration this
	// release runs (one that cannot destroy rows stays applied after the
	// code goes back; the earlier code does not read what it added).
	Stays []string `json:"stays,omitempty"`
}

// The ways a platform or Trade release is undone (infra docs/RELEASE.md,
// "这道检查生效之后怎么把平台退回旧提交", and "改坏了").
const (
	reversalRevert    = "revert the commits and land them through ready/; the revert is released like any change"
	reversalRerelease = "release again the commit the environment ran before (within a day of its STG run; after that, release it to STG first)"
)

// reversalOf is the reversal of a release decided as d.
func reversalOf(d Decision) *Reversal {
	r := &Reversal{Ways: []string{reversalRevert, reversalRerelease}}
	for _, f := range d.ReversibleDDL {
		r.Stays = append(r.Stays, f+": a migration that cannot destroy rows; it stays applied after a rollback")
	}
	for _, f := range d.AdditiveDDL {
		r.Stays = append(r.Stays, f+": additive DDL; what it added stays after a rollback")
	}
	return r
}
