package landing

import (
	"crypto/sha256"
	"fmt"
	"time"
)

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

// CIScopeMax bounds each CIScope value; a longer one is cut.
const CIScopeMax = 1500

type DrillObservation struct {
	Facts         Facts
	Main          string
	Candidate     CIRun
	WindowBlocked bool
	// PinCI: the operation needs a grant to reach main and its CI is not
	// pinned yet (C-5, review 698 B-01). Its pinned run is then the next
	// step unless CI has failed: kick once, then wait for pinned evidence.
	PinCI bool
}
type DrillAction struct{ Kind, Why string }

// The persisted op/try determines the run name before creation. Kubernetes
// rounds creationTimestamp to seconds, so it cannot order a same-second
// creation against the microsecond intent. Identity proves the attempt;
// the coarse time fence still rejects an object from an earlier second.
func rerunObserved(op string, o Operation, run string, created time.Time) bool {
	return !o.Rerun || o.RerunAt.IsZero() ||
		(run == CIName(op, 2) && !created.Before(o.RerunAt.Truncate(time.Second)))
}

// RootBudget survives process restarts, parked time and operation changes.
// n is the number of operations, including the current one. The eighth may
// finish, but opening the ninth is forbidden.
func RootBudget(s State, task string) (first time.Time, n int) {
	for _, o := range s.Operations {
		if o.Task == task {
			n++
			if first.IsZero() || o.ClaimedAt.Before(first) {
				first = o.ClaimedAt
			}
		}
	}
	if retry, ok := s.Retries[task]; ok {
		first = retry.At
	}
	return
}

// missingCIWait is how long an operation waits for its push's webhook CI
// before it makes its own. A platform land push starts none, so a platform
// operation waits only platformMissingCIWait: its own run must be created
// in a later second than PreparedAt, because a run counts only when its
// creation time (whole seconds) is after PreparedAt (review of R5:5.7-5
// round 2). Thirty seconds also covers clock skew between the controller
// and the API server.
const (
	missingCIWait         = 10 * time.Minute
	platformMissingCIWait = 30 * time.Second
	ciRunningParkAfter    = 30 * time.Minute
)

// WaitBoundaries exposes this binary's actual controller thresholds. It does
// not claim that a separately deployed workers binary has the same version.
func WaitBoundaries() map[string]time.Duration {
	return map[string]time.Duration{"infra_missing_ci": missingCIWait, "platform_missing_ci": platformMissingCIWait, "ci_running_park": ciRunningParkAfter, "root_execution": ClaimWindow, "approval_wait": WaitWindow}
}

// CIName is stable and DNS-safe even when op contains colons or slashes.
func CIName(op string, attempt int) string {
	sum := sha256.Sum256([]byte(op))
	return fmt.Sprintf("landing-ci-%x-%d", sum[:12], attempt)
}
