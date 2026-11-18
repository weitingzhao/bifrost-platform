package server

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/runhooks"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// runSummaryEnv switches the run summaries off ("off"); they are on otherwise.
const runSummaryEnv = "RUN_SUMMARY"

// runSummaryPrefixes are the run names a summary is written for: every run in
// cicd (CI runs ci-*, landings land-*, the rest bifrost-*). A hook needs a
// prefix; one hook each.
var runSummaryPrefixes = map[string]string{"ci": "ci-", "land": "land-", "bifrost": "bifrost-"}

// runSummaryReconcile is how often every finished run without a summary is
// given one: the bus wakes the writer on each ci.run.finished, and this
// catches what it missed (a restart, an event before the hook started).
const runSummaryReconcile = 15 * time.Minute

// startRunSummaries writes each finished run's task summary onto the run
// (cirunview.SummaryAnnotation; 2026-11-17, shared notes duty/from-ci-w1-
// 2026-11-17-taskrun-readers-questioned.md, step 1): woken by ci.run.finished,
// reconciled every 15 minutes. Nothing reads the summaries yet; the readers
// move to them one by one after reading both side by side. A summary that
// cannot be written is logged and retried by the next reconciliation; it never
// asks for the event again.
func (s *Server) startRunSummaries(ctx context.Context) {
	if s.delivery == nil || strings.TrimSpace(os.Getenv(runSummaryEnv)) == "off" {
		return
	}
	ns := s.delivery.PipelinesNamespace()
	for name, prefix := range runSummaryPrefixes {
		err := s.bus.runHooks.Register(runhooks.Hook{Name: "summary-" + name, Prefix: prefix, Handle: func(ctx context.Context, run, _ string) error {
			dyn, err := s.clusterDynamic()
			if err != nil {
				slog.Warn("run summary: no cluster client", "run", run, "error", err)
				return nil
			}
			if _, err := cirunview.WriteSummary(kubemetrics.WithSite(ctx, "run-summary"), dyn, ns, run); err != nil {
				slog.Warn("run summary: not written; the reconciliation tries again", "run", run, "error", err)
			}
			return nil
		}})
		if err != nil {
			slog.Error("run summary: hook", "prefix", prefix, "error", err)
		}
	}
	safego.Go("run-summary-reconcile", func() {
		refused := map[string]bool{} // runs the cluster refused to annotate, not read again by this process
		pass := func() {
			dyn, err := s.clusterDynamic()
			if err != nil {
				slog.Warn("run summary reconcile: no cluster client", "error", err)
				return
			}
			pctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			r, err := cirunview.WriteMissingSummaries(kubemetrics.WithSite(pctx, "run-summary"), dyn, ns, refused)
			if err != nil || r.Written > 0 || r.Failed > 0 {
				slog.Info("run summary reconcile", "runs", r.Runs, "written", r.Written, "failed", r.Failed, "forbidden", r.Forbidden, "refused_before", r.Refused, "first_error", r.FirstError, "error", err)
			}
		}
		safego.Do("run-summary-reconcile", pass)
		t := time.NewTicker(runSummaryReconcile)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				safego.Do("run-summary-reconcile", pass)
			}
		}
	})
	slog.Info("run summary: written onto each finished run in "+ns, "annotation", cirunview.SummaryAnnotation)
}
