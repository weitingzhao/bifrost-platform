package store

// NoMergeMission pins the mission the Owner reviewed, including its scope version.
type NoMergeMission struct {
	MissionID       string `json:"mission_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

// NoMergeRequest is one environment's atomic, approved disposal of old results.
// The manifest digest identifies the archive evidence; this action does not
// fetch archives, delete branches or mark any task as delivered.
type NoMergeRequest struct {
	Env                   string           `json:"env"`
	Missions              []NoMergeMission `json:"missions"`
	Reason                string           `json:"reason"`
	ArchiveManifestSHA256 string           `json:"archive_manifest_sha256"`
}

type NoMergeResult struct {
	MissionID string `json:"mission_id"`
	Version   int64  `json:"version"`
	NoMerge   string `json:"no_merge"`
	EventID   int64  `json:"event_id,omitempty"`
	Duplicate bool   `json:"duplicate"`
}
