package releasepolicy

// Tier is how much approval an action needs, as the approvals domain grades
// it: the same letters, so the server passes its grade over as Tier(grade).
// Only tier C can be covered by the policy; tier D always waits for the
// Owner, and the rest never ask. Until 2026-11-22 Decide took the approvals
// domain's own type (actions.Tier), which made releasepolicy import actions
// (the domain wall's row "releasepolicy -> approvals").
type Tier string

// The grades Decide tells apart; any other letter is refused. Callers pass
// the letter (Tier("C")): the letters are the contract, so no constants are
// exported.
const (
	tierC Tier = "C"
	tierD Tier = "D"
)
