package checklist

const stateVersion = "2026-07-19"

// Signal values aligned with FleetCellSignal.
const (
	SignalOK       = "ok"
	SignalDegraded = "degraded"
	SignalFail     = "fail"
	SignalUnknown  = "unknown"
)

// ItemSignal is one checklist item probe result.
type ItemSignal struct {
	ItemID string `json:"item_id"`
	Signal string `json:"signal"`
	Detail string `json:"detail,omitempty"`
	Env    string `json:"env,omitempty"`
}

// DispatchAction records what auto-dispatch decided for an item.
type DispatchAction struct {
	ItemID     string `json:"item_id"`
	Gate       string `json:"gate"` // auto | queue | notify | skip
	FixScope   string `json:"fix_scope,omitempty"`
	JobID      string `json:"job_id,omitempty"`
	QueueID    string `json:"queue_id,omitempty"`
	Detail     string `json:"detail,omitempty"`
	SkippedD10 bool   `json:"skipped_d10,omitempty"`
	At         string `json:"at,omitempty"` // RFC3339 — when this action was planned
	// When the item was actually dispatched, carried forward across the `skip`
	// actions a deduped run records. Without it the 24h window forgets after one
	// run and re-dispatches a condition that is still open.
	DispatchedAt string `json:"dispatched_at,omitempty"`
}

// FileRecord persists latest signals + KPI streak.
type FileRecord struct {
	Version            string           `json:"version"`
	UpdatedAt          string           `json:"updated_at"`
	LastRunID          string           `json:"last_run_id,omitempty"`
	Source             string           `json:"source,omitempty"`
	Signals            []ItemSignal     `json:"signals"`
	LastDispatch       []DispatchAction `json:"last_dispatch,omitempty"`
	QuietSuccessStreak int              `json:"quiet_success_streak"`
	LastFailAt         string           `json:"last_fail_at,omitempty"`
	LastAllOkAt        string           `json:"last_all_ok_at,omitempty"`
	History            []RunSummary     `json:"history,omitempty"`
}

type RunSummary struct {
	At      string `json:"at"`
	RunID   string `json:"run_id,omitempty"`
	Ok      int    `json:"ok"`
	Fail    int    `json:"fail"`
	Unknown int    `json:"unknown"`
	AllOk   bool   `json:"all_ok"`
}

type MergeRequest struct {
	Signals      []ItemSignal `json:"signals"`
	RunID        string       `json:"run_id,omitempty"`
	Source       string       `json:"source,omitempty"`
	AutoDispatch bool         `json:"auto_dispatch,omitempty"`
}

type SignalsResponse struct {
	UpdatedAt          string           `json:"updated_at"`
	LastRunID          string           `json:"last_run_id,omitempty"`
	Source             string           `json:"source,omitempty"`
	Signals            []ItemSignal     `json:"signals"`
	LastDispatch       []DispatchAction `json:"last_dispatch,omitempty"`
	QuietSuccessStreak int              `json:"quiet_success_streak"`
	LastFailAt         string           `json:"last_fail_at,omitempty"`
	LastAllOkAt        string           `json:"last_all_ok_at,omitempty"`
	NewFailures        []string         `json:"new_failures,omitempty"`
}

type KPIResponse struct {
	QuietSuccessStreak int          `json:"quiet_success_streak"`
	LastRunID          string       `json:"last_run_id,omitempty"`
	UpdatedAt          string       `json:"updated_at,omitempty"`
	LastFailAt         string       `json:"last_fail_at,omitempty"`
	LastAllOkAt        string       `json:"last_all_ok_at,omitempty"`
	LastCounts         *RunSummary  `json:"last_counts,omitempty"`
	RecentHistory      []RunSummary `json:"recent_history,omitempty"`
	NewFailHint        string       `json:"new_fail_hint,omitempty"`
}
