// Package codehealth stores code-health ratchet readings produced by
// agent-config/scripts/code-health/scan.sh and serves them to the Console.
//
// The scanner measures code assets; every other Observability signal measures
// runtime. That is the gap this package fills: a green Control Room says the
// cluster is up, not that the code inside it is still maintainable.
package codehealth

import "time"

// Metric is one ratchet reading. Value/Baseline carry the ratchet contract:
// value > baseline is a regression, value < baseline means the baseline owes a
// lowering.
type Metric struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Domain      string `json:"domain"`
	Repo        string `json:"repo"`
	Value       int    `json:"value"`
	Baseline    int    `json:"baseline"`
	BaselineVar string `json:"baseline_var,omitempty"` // baselines.env key; required to lower
	Status      string `json:"status"`                 // over | at_baseline | improved
	Detail      string `json:"detail,omitempty"`
}

// Report is one scan.sh run.
//
// Commit is the freshness anchor: a report whose commit is behind the repo's
// current HEAD describes code that no longer exists, and the Console must say
// so rather than present it as the current state.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Commit      string    `json:"commit"`
	NotMeasured string    `json:"not_measured,omitempty"`
	Source      string    `json:"source,omitempty"` // local | ci
	Metrics     []Metric  `json:"metrics"`
	ReceivedAt  time.Time `json:"received_at"`
}

// StatusResponse is what GET returns.
//
// Reported is explicit rather than implied by an empty Metrics slice: a client
// must be able to tell "nothing has ever been measured" apart from "measured
// and found clean". Conflating the two is how a dashboard reports health it
// never observed.
type StatusResponse struct {
	Reported   bool       `json:"reported"`
	Note       string     `json:"note,omitempty"`
	Latest     *Report    `json:"latest,omitempty"`
	History    []Report   `json:"history,omitempty"`
	Freshness  *Freshness `json:"freshness,omitempty"`
}

// Freshness tells the Console whether Agent cut planning should trust the
// stored reading, or Live Re-scan first. RescanAvailable is true only when this
// API host can see the multi-repo workspace and run scan.sh (typical: local DEV).
type Freshness struct {
	RescanAvailable bool   `json:"rescan_available"`
	WorkspaceRoot   string `json:"workspace_root,omitempty"`
	InfraHead       string `json:"infra_head,omitempty"`
	ReadingCommit   string `json:"reading_commit,omitempty"`
	StaleVsHead     bool   `json:"stale_vs_head"`
	Note            string `json:"note,omitempty"`
}
