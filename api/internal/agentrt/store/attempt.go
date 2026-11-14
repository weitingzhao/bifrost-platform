package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Attempts.

// Fence names the caller of an attempt route: the host that holds the
// attempt and the epoch it was given.
type Fence struct {
	AttemptID string
	HostID    string
	Epoch     int64
}

// Beat is a heartbeat's answer.
type Beat struct {
	LeaseExpiresAt time.Time
	Stop           bool // a stop control covers this attempt: end it
}

// Ending is how an executor ended, with what it used and what its vendor
// said about its pools (2.4 slice ①); an agentd older than 2.4 sends neither.
type Ending struct {
	ExitCode           *int
	EndHow, Conclusion string
	Usage              *Usage
	Pools              []PoolReading
	SelfCheck          *SelfCheck
}

// SelfCheck is how an implementation's executor ran the spec's hand-in
// commands in its own session, as agentd counted them over the attempt
// (R5:5.7, the check value table): results seen, those that exited non-zero,
// a red command's later exit 0, whether the last result of every hand-in
// command that ran exited 0, and the seconds from run to result, summed.
type SelfCheck struct {
	Runs      int  `json:"runs"`
	Red       int  `json:"red"`
	Fixed     int  `json:"fixed"`
	LastGreen bool `json:"last_green"`
	Seconds   int  `json:"seconds"`
}

// BranchHead reads the commit a branch of a repository on the main Git
// server points at. The HTTP layer passes a Gitea reader, tests a fake.
type BranchHead func(ctx context.Context, repo, ref string) (string, error)

// ErrNoHead: no reader of branch heads is configured, so a pushed branch
// cannot be checked yet; agentd tries again later.
var ErrNoHead = errors.New("store: no reader of branch heads is configured")

// Result is what agentd hands over: the report, other artifacts, and the
// branch and commit it pushed, if it pushed one.
type Result struct {
	Report    string
	Artifacts []string
	Ref       string
	Commit    string
}

// reportKinds are the kinds a result's report may be stored as.
var reportKinds = map[string]bool{"report": true, "verify": true, "review": true}

func haveArtifacts(ctx context.Context, tx pgx.Tx, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var missing []string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id ORDER BY id), '{}') FROM unnest($1::text[]) AS id
	  WHERE NOT EXISTS (SELECT 1 FROM agentrt.artifact WHERE artifact_id = id)`, ids).Scan(&missing); err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: artifacts not stored: %v", ErrState, missing)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
