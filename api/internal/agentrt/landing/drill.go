package landing

import "time"

// CIRun is trusted CI evidence, including creation time and exact revision.
// Missing and Unknown are distinct; no successful historical run overrides a
// newer Failed/Running/Unknown run (CUT §2.5.5 and §2.10).
type CIRun struct {
	Run, R, Status string
	// Accepted same-tree reuse provenance; empty for ordinary runs.
	Tree, TestedBy, TestedUID string
	CreatedAt                 time.Time
	// UID and the run's results (CUT §11.7 item 3): what a STG snapshot
	// pins P and U from, read from the same run. Empty when the run has
	// not written them (yet).
	UID, PlatformCommit, UICommit string
	// InfraCommit is the run's infra-commit result: a landing grant's
	// material names every sibling checkout the run built with (C-5).
	InfraCommit string
	// Results is present only on infra's repository-aware evidence.
	Results map[string]string
	// Scope is what a platform run's test-api ran; empty when the run
	// gives none of its results.
	Scope CIScope
}

// CIScope is a platform run's api-scope, api-packages, api-store-run and
// api-scope-why results.
type CIScope struct {
	Scope, Packages, StoreRun, Why string
}

// Historical budgets retained for the delivery boundary view.
const ClaimWindow = 6 * time.Hour
const WaitWindow = 72 * time.Hour

const (
	missingCIWait         = 10 * time.Minute
	platformMissingCIWait = 30 * time.Second
	ciRunningParkAfter    = 30 * time.Minute
)

// WaitBoundaries keeps the historical thresholds shown by the delivery view.
// These values do not configure the current landing pipeline.
func WaitBoundaries() map[string]time.Duration {
	return map[string]time.Duration{"infra_missing_ci": missingCIWait, "platform_missing_ci": platformMissingCIWait, "ci_running_park": ciRunningParkAfter, "root_execution": ClaimWindow, "approval_wait": WaitWindow}
}
