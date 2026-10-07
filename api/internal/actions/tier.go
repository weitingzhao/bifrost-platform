package actions

import "strings"

// Tier is the ADR §5 action level.
type Tier string

const (
	TierA Tier = "A"
	TierB Tier = "B"
	TierC Tier = "C"
	TierD Tier = "D"
	TierX Tier = "X"
)

// Valid reports whether t is one of the ADR levels.
func (t Tier) Valid() bool {
	switch t {
	case TierA, TierB, TierC, TierD, TierX:
		return true
	default:
		return false
	}
}

// NeedsApproval is true for levels that must not run on the direct endpoint
// until an executed approval with the same params exists.
func (t Tier) NeedsApproval() bool {
	return t == TierC || t == TierD
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
