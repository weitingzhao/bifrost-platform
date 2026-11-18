// Package workregistration owns the recorded work shape and read-only session bindings.
// Registration authorization and writes remain in their existing store path.
//
// What it offers other domains, all read in the caller's transaction:
// Snapshot and ActivityBuckets (a root's registered work, each map holding the
// JSON fields of Note plus description, recorded_at, heartbeat_age_s, status,
// elapsed_s and backfilled); RecentStreams and RecentRecords (the events of a
// root's work its recent changes list); HasWork and Registrants (whether a
// root has work, and who registered it). Other domains read a work's records
// through these and do not name its events' shape in SQL of their own.
package workregistration

// Note is the persisted external-work record. Readers decode the whole shape
// so malformed non-projected fields are not silently accepted.
type Note struct {
	V     int    `json:"v"`
	Kind  string `json:"kind"`
	ID    string `json:"external_work_id"`
	Actor string `json:"registrant"`
	// A collector's work only (§1.4): the host the server writes from the
	// collector's token, and the identity it was registered with. A note
	// without them is any other registrant's, or a collector's from before.
	Host               string   `json:"host,omitempty"`
	Vendor             string   `json:"vendor,omitempty"`
	Session            string   `json:"session,omitempty"`
	Epoch              int64    `json:"epoch,omitempty"`
	Sidebar            string   `json:"sidebar,omitempty"`
	Root               string   `json:"root"`
	Node               string   `json:"plan_node_id"`
	RunKey             string   `json:"source_run_key"`
	Artifact           string   `json:"artifact_id"`
	Mission            string   `json:"mission_id,omitempty"`
	Task               string   `json:"task_id,omitempty"`
	Seq                int64    `json:"seq"`
	Type               string   `json:"type"`
	State              string   `json:"state"`
	RequestDigest      string   `json:"request_digest"`
	RegistrationDigest string   `json:"registration_digest"`
	ObservedAt         string   `json:"observed_at,omitempty"`
	RegisteredAt       string   `json:"registered_at"`
	SourceStartedAt    string   `json:"source_started_at,omitempty"`
	StartedAt          string   `json:"started_at,omitempty"`
	HeartbeatAt        string   `json:"heartbeat_at"`
	EndedAt            string   `json:"ended_at,omitempty"`
	ReportPathDigest   string   `json:"report_path_digest,omitempty"`
	ReportDigest       string   `json:"report_digest,omitempty"`
	Commits            []string `json:"commit_shas,omitempty"`
	ExitCode           *int     `json:"exit_code,omitempty"`
}
