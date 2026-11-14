package store

import (
	"strings"
	"unicode/utf8"
)

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

// Validate checks the signed request before filing it and again at execution.
func (r NoMergeRequest) Validate() error {
	switch {
	case r.Env != "stg" && r.Env != "prod":
		return invalid("env must be stg or prod")
	case len(r.Missions) == 0 || len(r.Missions) > 100:
		return invalid("missions must contain 1 to 100 entries")
	case strings.TrimSpace(r.Reason) == "" || len(r.Reason) > MaxNoMergeBytes || !utf8.ValidString(r.Reason) || strings.ContainsRune(r.Reason, 0):
		return invalid("reason must contain 1 to %d UTF-8 bytes", MaxNoMergeBytes)
	case !sha256RE.MatchString(r.ArchiveManifestSHA256):
		return invalid("archive_manifest_sha256 must be a lowercase SHA-256 digest")
	}
	seen := map[string]bool{}
	for _, m := range r.Missions {
		if strings.TrimSpace(m.MissionID) == "" || strings.TrimSpace(m.MissionID) != m.MissionID || strings.ContainsRune(m.MissionID, 0) || m.ExpectedVersion < 1 || seen[m.MissionID] {
			return invalid("each mission needs a unique mission_id and a positive expected_version")
		}
		seen[m.MissionID] = true
	}
	return nil
}

type NoMergeResult struct {
	MissionID string `json:"mission_id"`
	Version   int64  `json:"version"`
	NoMerge   string `json:"no_merge"`
	EventID   int64  `json:"event_id,omitempty"`
	Duplicate bool   `json:"duplicate"`
}
