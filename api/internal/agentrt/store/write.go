package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// InTx runs fn in one transaction and commits it; any error rolls back
// (eventlog.InTx).
func InTx(ctx context.Context, conn interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, fn func(pgx.Tx) error) error {
	return eventlog.InTx(ctx, conn, fn)
}

// MissionStream names the event stream of a mission (PlanStream, of a plan
// tree, is in plannode.go).
func MissionStream(missionID string) string { return "mission:" + missionID }

// Task is a task to propose. Core and Needs decide who may take it (v5
// §5.1.2 「分流顺序」); Repo and BaseCommit are where agentd starts from.
type Task struct {
	Wave                                                                                      string
	Urgent                                                                                    bool
	ID, MissionID, PlanNode, OffPlanReason, Subject, Role, Class, Title, SpecArtifact, Effect string
	Repo, BaseCommit                                                                          string
	Core                                                                                      bool
	Needs                                                                                     []string
	HardLimit                                                                                 time.Duration
	Token                                                                                     string // the proposing token, when a route did
	// pin, viaSession and pinReason: the executor pin, the session header
	// and the reason (pinreason.go) of a pinned proposal, set only by proposeBatchTx after checkPin (pin.go);
	// nothing outside this package, and nothing else in it, sets them.
	pin, viaSession, pinReason string
	providers                  []string
}
