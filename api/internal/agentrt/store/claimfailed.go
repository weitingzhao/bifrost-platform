package store

import ()

// What a retried implementation is told of the rounds before it (TD-405).
//
// Each round of an implementation starts from the task's base commit with
// the same spec. The check that sent the last round back reached only the
// card and the reviewer, so on PROD M-23 codex handed in the same compile
// error three rounds running, and the Owner's "one more round" bought
// nothing. Now the claim of an implementation in a round after the first
// carries the evidence of the task's last deterministic check and of its
// last review, each when it failed: agentd reads them (verify.json, masked
// of local paths when agentd handed it in, and the review's report) and puts
// them after the spec in the executor's prompt. A failure the STG test
// switch injected (TestNeedFailFirstVerify) says nothing of the work and is
// left out.

// FailedCheck is a check an earlier round of a task failed: its method
// (deterministic, review, or landing_ci: the attempt's red landing CI,
// precheck.go), the number of the attempt it checked, and the artifact of
// its report (the verification's evidence).
type FailedCheck struct {
	Method    string
	AttemptNo int
	Evidence  string
}
