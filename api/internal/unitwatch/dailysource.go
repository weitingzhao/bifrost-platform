package unitwatch

import (
	"context"
	"errors"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// DailyPassTimeout bounds one pass of the daily unit run.
const DailyPassTimeout = 10 * time.Minute

// DailyDeps is what the daily unit run stands on; each is read when the pass
// runs. DB returns nil when the ledger is not connected.
type DailyDeps struct {
	Head        func(ctx context.Context, repo string) (string, error)
	FirstParent func(ctx context.Context, repo, commit string) (string, error)
	DB          func() eventlog.DB
	Trees       func() unitexec.TreesFunc
	Cluster     func() unitexec.Cluster
	Namespace   func() string
	// Placed is the dispatch configuration with the node the units go to now.
	Placed func(ctx context.Context) (unitexec.Config, error)
}

// DailySource is the controller's daily unit run.
func DailySource(deps DailyDeps) func(context.Context) error {
	d := &Daily{
		Head:        deps.Head,
		FirstParent: deps.FirstParent,
		Now:         time.Now,
		Start: func(ctx context.Context, repo, commit string) error {
			cfg, err := deps.Placed(ctx)
			if err != nil {
				return err
			}
			_, err = unitexec.Advance(ctx, deps.DB(), deps.Trees(), deps.Cluster(), cfg, repo, commit, "nightly")
			return err
		},
		Tasks: func(ctx context.Context, repo, commit string) ([]DailyTask, error) {
			plan, err := unitplan.BuildFrom(ctx, deps.Trees(), repo, commit)
			if err != nil {
				return nil, err
			}
			out := make([]DailyTask, 0, len(plan.Units))
			for _, u := range plan.Units {
				out = append(out, DailyTask{Unit: u.Unit, Task: u.Task, Key: u.Key})
			}
			return out, nil
		},
		Verdict: func(ctx context.Context, repo string, t DailyTask) (string, error) {
			rows, err := evidence.EvidenceByKey(ctx, deps.DB(), repo, t.Unit, t.Task, t.Key)
			if err != nil {
				return "", err
			}
			return evidence.EvidenceVerdict(rows), nil
		},
	}
	return func(ctx context.Context) error {
		db := deps.DB()
		if db == nil {
			return nil
		}
		if unitexec.ConfigFromEnv(deps.Namespace()).Mode == "off" {
			return nil
		}
		d.DB = db
		ctx, cancel := context.WithTimeout(ctx, DailyPassTimeout)
		defer cancel()
		// The day stays unmarked, so a later pass starts the run.
		if err := d.Pass(ctx); err != nil && !errors.Is(err, unitexec.ErrNoNode) {
			return err
		}
		return nil
	}
}
