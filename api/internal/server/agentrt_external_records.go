package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/approvalfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/readslot"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/runfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
)

// wireExternalRecords supplies bounded, read-only lists for durable backfill.
func (s *Server) wireExternalRecords(src *controller.Sources) {
	src.Questions = s.questionRuntime.Observations()
	if s.approvals != nil {
		reads := readslot.New(2)
		src.Approvals = func(ctx context.Context) ([]seenfacts.ApprovalFact, error) {
			items, ok, err := within(ctx, reads, func() ([]approvals.Approval, bool, error) {
				items, err := s.approvals.Observations()
				return items, true, err
			})
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("approval observations unavailable")
			}
			now := time.Now()
			var out []seenfacts.ApprovalFact
			for _, a := range items {
				if f, ok := approvalfacts.Observation(a, now); ok {
					out = append(out, f)
				}
			}
			return out, nil
		}
	}
	src.UnitRuns = s.unitRunsSource()
	src.UnitsDaily = s.unitsDailySource()
	if s.delivery != nil && os.Getenv("AGENTRT_ENV") == "prod" {
		// One platform chooses: PROD, as it is PROD that starts the deliveries.
		src.CINode = s.delivery.PublishCINode
	}
	src.HeadRelease = s.bus.head.Serial(s.headReleaseSource())
	if s.delivery != nil {
		src.RunEvidenced = s.unitScopeShadowSource()
		src.CommitTree = giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})
		src.RunFailure = func(ctx context.Context, run string) ([]string, []string, string, error) {
			failed, err := s.delivery.RunFailedTasks(kubemetrics.WithSite(ctx, "records"), run)
			if err != nil {
				return nil, nil, "", err
			}
			tasks, tests, class := runFailure(failed)
			return tasks, tests, class, nil
		}
		src.PipelineRun = func(ctx context.Context, name string) (seenfacts.PipelineRun, bool, error) {
			item, ok, err := s.delivery.PipelineRunObservation(kubemetrics.WithSite(ctx, "records"), name)
			if err != nil || !ok {
				return seenfacts.PipelineRun{}, false, err
			}
			f, ok := runfacts.PipelineRun(item)
			return f, ok, nil
		}
		src.PipelineRuns = func(ctx context.Context) ([]seenfacts.PipelineRun, error) {
			items, err := s.delivery.PipelineRunObservations(kubemetrics.WithSite(ctx, "records"))
			if err != nil {
				return nil, err
			}
			var out []seenfacts.PipelineRun
			for _, item := range items {
				if f, ok := runfacts.PipelineRun(item); ok {
					out = append(out, f)
				}
			}
			return out, nil
		}
	}
}

// pipelineRecordsEveryEnv sets how often the workers list the retained
// pipeline runs for the ones that completed, and pipelineRecordsFullEveryEnv
// how often such a pass compares every listed run with the database whatever
// it remembers: Go durations (30s, 5m), not under pipelineRecordsEveryMin.
// Unset, the controller's defaults hold (controller.DefaultPipelineEvery,
// controller.DefaultPipelineFullEvery).
const (
	pipelineRecordsEveryEnv     = "AGENTRT_PIPELINE_RECORDS_EVERY"
	pipelineRecordsFullEveryEnv = "AGENTRT_PIPELINE_RECORDS_FULL_EVERY"
	pipelineRecordsEveryMin     = 5 * time.Second
)

// pipelineRecordsDuration reads one of them; zero is the controller's default.
func pipelineRecordsDuration(name string) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < pipelineRecordsEveryMin {
		slog.Error("agentrt: "+name+" is not a duration of at least "+pipelineRecordsEveryMin.String()+"; the default holds", "value", v)
		return 0
	}
	return d
}

// wirePipelineRecords sets how often the controller records completed runs.
func (s *Server) wirePipelineRecords(c *controller.Controller) {
	c.PipelineEvery = pipelineRecordsDuration(pipelineRecordsEveryEnv)
	c.PipelineFullEvery = pipelineRecordsDuration(pipelineRecordsFullEveryEnv)
	// Bus step 2: gate-cluster names each finished run (startGateCluster).
	c.PipelineFinished = s.bus.finished.Queue()
}

// runFailure adapts delivery observations to the CI failure projection.
func runFailure(failed []delivery.FailedTask) (tasks, tests []string, class string) {
	var observations []cirunview.FailedTask
	for _, f := range failed {
		observations = append(observations, cirunview.FailedTask{Task: f.Task, Tests: f.Tests, NamesTests: f.NamesTests})
	}
	return cirunview.Failure(observations)
}
