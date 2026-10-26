package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
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

// MarkMissionsNoMerge writes each mission_amended event and scope change in
// one transaction. Only done missions qualify. Matching values at the expected
// version are no-ops; an exact retry of this approval may also observe the
// version it wrote. All other stale versions and existing values are refused.
func MarkMissionsNoMerge(ctx context.Context, db DB, r NoMergeRequest, approvalID string) ([]NoMergeResult, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if !approvalIDRE.MatchString(approvalID) {
		return nil, invalid("no_merge requires an approval id")
	}
	missions := slices.Clone(r.Missions)
	slices.SortFunc(missions, func(a, b NoMergeMission) int { return strings.Compare(a.MissionID, b.MissionID) })
	var out []NoMergeResult
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		for _, m := range missions {
			result, err := markMissionNoMerge(ctx, tx, m, r, approvalID)
			if err != nil {
				return err
			}
			out = append(out, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func markMissionNoMerge(ctx context.Context, tx pgx.Tx, m NoMergeMission, r NoMergeRequest, approvalID string) (NoMergeResult, error) {
	var state string
	var version int64
	var present, equal bool
	// Match other mission writers: row before stream. Stable mission ordering
	// prevents two batches from taking these locks in opposite order.
	err := tx.QueryRow(ctx, `SELECT state, version, scope_json ? 'no_merge',
	  COALESCE(scope_json->'no_merge' = to_jsonb($2::text), false)
	  FROM agentrt.mission WHERE mission_id=$1 FOR NO KEY UPDATE`, m.MissionID, r.Reason).Scan(&state, &version, &present, &equal)
	out := NoMergeResult{MissionID: m.MissionID, Version: version, NoMerge: r.Reason}
	if errors.Is(err, pgx.ErrNoRows) {
		return out, invalid("mission %s does not exist", m.MissionID)
	}
	if err != nil {
		return out, err
	}
	if state != "done" {
		return out, fmt.Errorf("%w: mission %s is %s, not done", ErrState, m.MissionID, state)
	}
	if version != m.ExpectedVersion {
		// Only the exact committed write can explain a stale version on retry.
		if equal && version == m.ExpectedVersion+1 {
			err = tx.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE mission_id=$1 AND event_type='mission_amended'
			  AND body_json->>'approval_id'=$2 AND body_json->>'no_merge'=$3
			  AND body_json->>'archive_manifest_sha256'=$4 AND body_json->>'env'=$5
			  AND body_json->>'expected_version'=$6 AND body_json->>'version'=$7 ORDER BY seq DESC LIMIT 1`,
				m.MissionID, approvalID, r.Reason, r.ArchiveManifestSHA256, r.Env, fmt.Sprint(m.ExpectedVersion), fmt.Sprint(version)).Scan(&out.EventID)
			if err == nil {
				out.Duplicate = true
				return out, nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return out, err
			}
		}
		return out, fmt.Errorf("%w: mission %s version is %d, expected %d", ErrState, m.MissionID, version, m.ExpectedVersion)
	}
	if present {
		if !equal {
			return out, fmt.Errorf("%w: mission %s already has a different no_merge", ErrState, m.MissionID)
		}
		out.Duplicate = true
		return out, nil
	}
	ev, err := Append(ctx, tx, Event{Stream: MissionStream(m.MissionID), Type: "mission_amended", Actor: "controller", MissionID: m.MissionID,
		Body: map[string]any{"reason": r.Reason, "no_merge": r.Reason, "approval_id": approvalID,
			"archive_manifest_sha256": r.ArchiveManifestSHA256, "env": r.Env, "expected_version": version, "version": version + 1}})
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET scope_json=jsonb_set(scope_json, '{no_merge}', to_jsonb($2::text)), version=version+1 WHERE mission_id=$1`, m.MissionID, r.Reason)
	out.Version, out.EventID = version+1, ev.EventID
	return out, err
}
