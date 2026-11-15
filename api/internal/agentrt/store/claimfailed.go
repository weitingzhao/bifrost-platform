package store

// Historical failed-check shape retained for recorded task rounds.
// The old dispatcher no longer injects these reports into executor prompts.

// FailedCheck is a check an earlier round of a task failed: its method
// (deterministic, review, or landing_ci: the attempt's red landing CI,
// precheck.go), the number of the attempt it checked, and the artifact of
// its report (the verification's evidence).
type FailedCheck struct {
	Method    string
	AttemptNo int
	Evidence  string
}
