package landing

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"time"
)

// CIRun is trusted CI evidence, including creation time and exact revision.
// Missing and Unknown are distinct; no successful historical run overrides a
// newer Failed/Running/Unknown run (CUT §2.5.5 and §2.10).
type CIRun struct {
	Run, R, Status string
	CreatedAt      time.Time
	// UID and the run's results (CUT §11.7 item 3): what a STG snapshot
	// pins P and U from, read from the same run. Empty when the run has
	// not written them (yet).
	UID, PlatformCommit, UICommit string
}

func LatestCI(runs []CIRun, revision string, after time.Time) CIRun {
	matches := []CIRun{}
	for _, r := range runs {
		if r.R == revision && r.CreatedAt.After(after) {
			matches = append(matches, r)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].CreatedAt.Equal(matches[j].CreatedAt) {
			return matches[i].Run > matches[j].Run
		}
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})
	if len(matches) == 0 {
		return CIRun{R: revision, Status: "Missing"}
	}
	newest := matches[0]
	// Creation times can tie. Without an ordering fact, conflicting tied runs
	// are unknown, never a success selected by an arbitrary run name.
	for _, r := range matches[1:] {
		if r.CreatedAt.Equal(newest.CreatedAt) && r.Status != newest.Status {
			newest.Status = "Unknown"
		}
	}
	switch newest.Status {
	case "Succeeded", "Failed", "Running", "Unknown":
	default:
		newest.Status = "Unknown"
	}
	return newest
}

type DrillObservation struct {
	Facts             Facts
	Main              string
	Candidate, MainCI CIRun
	WindowBlocked     bool
}
type DrillAction struct{ Kind, Why string }

// NextDrill checks main even while parked on an old red main. This pure
// judgement is persisted through the v1.1 discarded/rehearsed transitions.
func NextDrill(s State, op string, obs DrillObservation, now time.Time) DrillAction {
	o := s.Operations[op]
	if o.Terminal() {
		return DrillAction{Kind: "cleanup"}
	}
	if o.AtMain() {
		return DrillAction{Kind: "wait"} // only the main reconciler may settle an intent
	}
	_, n := RootBudget(s, o.Task)
	if why := RootBound(s, o.Task, now, false); why != "" {
		return DrillAction{Kind: "failed", Why: why}
	}
	if o.Phase == "awaiting_grant" {
		return DrillAction{Kind: "wait"}
	}
	if obs.Main == "" {
		return DrillAction{Kind: "wait"}
	}
	if o.R != "" && obs.Main != o.M0 {
		if n >= 8 {
			return DrillAction{Kind: "failed", Why: "bound"}
		}
		return DrillAction{Kind: "replay"}
	}
	d := EligibleToClaim(obs.Facts, now) // preparation may carry soft failures to the locked final gate
	if !d.Allowed {
		switch d.Reason {
		case "rule", "limit":
			return DrillAction{Kind: "parked", Why: d.Reason}
		case "floor":
			return DrillAction{Kind: "failed", Why: "floor"}
		}
		return DrillAction{Kind: "failed", Why: "stale"}
	}
	if o.Phase == "parked" && o.Why == "rule" {
		return DrillAction{Kind: "unparked"}
	}
	ci := obs.Candidate
	if ci.Status == "Unknown" || ci.CreatedAt.After(now) {
		return DrillAction{Kind: "wait"}
	}
	if ci.R != o.R || !ci.CreatedAt.After(o.PreparedAt) {
		ci = CIRun{Status: "Missing"}
	}
	if !rerunObserved(op, o, ci.Run, ci.CreatedAt) {
		return DrillAction{Kind: "wait"}
	}
	switch ci.Status {
	case "Missing":
		if !o.Kicked && !now.Before(o.PreparedAt.Add(10*time.Minute)) {
			return DrillAction{Kind: "ci_kick"}
		}
	case "Running":
		if !now.Before(ci.CreatedAt.Add(30 * time.Minute)) {
			return DrillAction{Kind: "parked", Why: "ci_stuck"}
		}
	case "Succeeded":
		if obs.WindowBlocked {
			return DrillAction{Kind: "parked", Why: "window"}
		}
		if o.Phase == "parked" {
			return DrillAction{Kind: "unparked"}
		}
		return DrillAction{Kind: "ready"}
	case "Failed":
		switch obs.MainCI.Status {
		case "Failed":
			return DrillAction{Kind: "parked", Why: "main_red"}
		case "Succeeded":
			if o.Phase == "parked" {
				return DrillAction{Kind: "unparked"}
			}
			if !o.Rerun {
				return DrillAction{Kind: "ci_rerun"}
			}
			return DrillAction{Kind: "failed", Why: "ci_red"}
		}
	}
	return DrillAction{Kind: "wait"}
}

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

// CIName is stable and DNS-safe even when op contains colons or slashes.
func CIName(op string, attempt int) string {
	sum := sha256.Sum256([]byte(op))
	return fmt.Sprintf("landing-ci-%x-%d", sum[:12], attempt)
}
