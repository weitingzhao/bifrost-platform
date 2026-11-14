package store

import ()

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
