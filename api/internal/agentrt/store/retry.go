package store

// Historical policy-alert phrases: the closed phrases the old rows record.
// The dispatcher no longer retries, withdraws or reassigns executors, and an
// artifact refusal is artifact.RecordRefusal's own event type since
// 2026-11-17 (artifact_refused).

// Policy alert phrases of this file: the closed phrases the event records.
const (
	repeatedFailurePhrase = "failed three times in a row; not offered the task again"
	localPathPhrase       = "a local path (H8)"
	workspaceRefPhrase    = "another task's workspace"
)
