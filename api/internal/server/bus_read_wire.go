package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/acceptance"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/drill"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/effect"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/guardblock"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/observe"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/privilege"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/subscribe"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
)

// The bus's read routes live in the bus packages (bus/regressions,
// bus/registry, bus/observe, bus/effect, bus/subscribe); this file hands
// them what they stand on.

func (s *Server) busRegressions() regressions.API {
	return regressions.API{DB: s.bus.db, Who: writerOf}
}

func (s *Server) busPrivilege() privilege.API {
	return privilege.API{DB: s.bus.db, Who: writerOf}
}

func (s *Server) busAcceptance() acceptance.API {
	return acceptance.API{DB: s.bus.db, Who: writerOf}
}

func (s *Server) busGuardBlocks() guardblock.API {
	return guardblock.API{DB: s.bus.db, Who: writerOf}
}

func (s *Server) busObserve() observe.Handler {
	return observe.Handler{
		Pool:    s.bus.pool.Load,
		Letters: coord.ReadLetters,
		Ledger: func(pool *pgxpool.Pool) relay.Ledger {
			return &relay.PGLedger{DB: func() relay.TxBeginner { return pool }, EventsAfter: busEventsAfter}
		},
		CursorAt: func(pool *pgxpool.Pool) func(context.Context, time.Time) (relay.Cursor, error) {
			return relay.CursorAt(pool)
		},
		Gate: s.bus.gate.Load,
	}
}

func (s *Server) busEffect() effect.Deps {
	d := effect.Deps{ProblemEvidence: effect.ProblemEvidence(s.bus.db, drill.Stream), Promql: s.promql}
	if s.delivery != nil {
		d.CachedRuns = s.delivery.CachedRunsOf
	}
	if s.recordDB() != nil {
		d.Wakes = func(ctx context.Context, since time.Time) (effect.Wakes, error) {
			db := s.recordDB()
			if db == nil {
				return effect.Wakes{}, errors.New("the agent runtime database is not connected")
			}
			w, err := coord.Wakes(ctx, db, since)
			return effect.Wakes{Delivered: w.Delivered, Undeliverable: w.Undeliverable, Acked: w.Acked, AckMedianSecs: w.AckMedianSecs, BySeat: w.BySeat}, err
		}
	}
	return d
}

// handleBusEffect and handleBusNextgen build their deps per request: the
// delivery reader and the record database are set after the routes mount.
func (s *Server) handleBusEffect(w http.ResponseWriter, r *http.Request) {
	s.busEffect().HandleEffect(w, r)
}

func (s *Server) handleBusNextgen(w http.ResponseWriter, r *http.Request) {
	s.busEffect().HandleNextgen(w, r)
}

// earnedReadRoutes: the display routes the run cache's trust is earned on.
func (s *Server) earnedReadRoutes(ctx context.Context) map[string]bool {
	if s.telemetry == nil {
		return nil
	}
	return s.busEffect().EarnedReadRoutes(ctx, delivery.CacheRoutes(), delivery.ReadersCompareN())
}

// busRunDetail answers a subscription row's params.detail.
func (s *Server) busRunDetail() subscribe.Detail {
	var taskRuns func(ctx context.Context, run, task string) ([]unstructured.Unstructured, error)
	if s.delivery != nil {
		taskRuns = func(ctx context.Context, run, task string) ([]unstructured.Unstructured, error) {
			return s.delivery.UnitTaskRuns(kubemetrics.WithSite(ctx, "bus.detail"), "",
				"tekton.dev/pipelineRun="+run+",tekton.dev/pipelineTask="+task)
		}
	}
	return subscribe.RunDetail(s.busRegressions().OpenDetail, taskRuns)
}

// openRegressions is the release context's reader: repo's known regressions
// on main with no fix recorded, newest first.
func (s *Server) openRegressions(ctx context.Context, repo string) ([]delivery.KnownRegression, error) {
	open, err := s.busRegressions().Open(ctx, repo)
	if err != nil {
		return nil, err
	}
	return knownRegressions(open), nil
}

func knownRegressions(open []regressions.Regression) []delivery.KnownRegression {
	out := make([]delivery.KnownRegression, 0, len(open))
	for _, g := range open {
		out = append(out, delivery.KnownRegression{Introduced: g.Introduced, Impact: g.Impact, By: g.By, KnownAt: g.KnownAt.UTC().Format(time.RFC3339)})
	}
	return out
}

// handleRegressionWrite is the report_regression / mark_regression_fixed
// handler (actions_wire.go registers it).
func (s *Server) handleRegressionWrite(fixed bool) http.HandlerFunc {
	return s.busRegressions().HandleWrite(fixed)
}

func (s *Server) promql(ctx context.Context, q string) (map[string]float64, error) {
	if s.telemetry == nil {
		return nil, fmt.Errorf("no Prometheus reader on this platform")
	}
	return s.telemetry.PromQLSum(ctx, q, effect.PromqlLabels)
}

// handleBusProblems is GET /api/v1/bus/problems (coord.ProblemsRoute).
func (s *Server) handleBusProblems(w http.ResponseWriter, r *http.Request) {
	coord.ProblemsRoute{DB: s.recordDB}.ServeHTTP(w, r)
}
