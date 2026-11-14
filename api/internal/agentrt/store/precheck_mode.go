package store

import (
	"fmt"
	"strings"
)

// Precheck is how the controller checks a submitted implementation
// (AGENTRT_PRECHECK): check, the deterministic check whose pass accepts;
// verify, the deterministic check then a review. ci, the landing CI's own
// run, ended with the landing lane (2026-11-14) and now acts as check.
type Precheck string

const (
	PrecheckCI     Precheck = "ci"
	PrecheckVerify Precheck = "verify"
	PrecheckCheck  Precheck = "check"
)

// ParsePrecheck reads AGENTRT_PRECHECK; "" is check.
func ParsePrecheck(s string) (Precheck, error) {
	switch p := Precheck(strings.TrimSpace(s)); p {
	case "", PrecheckCheck:
		return PrecheckCheck, nil
	case PrecheckCI, PrecheckVerify:
		return p, nil
	}
	return PrecheckCheck, fmt.Errorf("AGENTRT_PRECHECK is check, ci or verify, not %q", s)
}

// reviews: a review follows the deterministic check.
func (p Precheck) reviews() bool { return p == PrecheckVerify }
