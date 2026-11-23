package server

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/approvalfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/runfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	verificationassembly "github.com/weitingzhao/bifrost-platform/api/internal/verification/assembly"
)

// wireExternalRecords supplies bounded, read-only lists for durable backfill.
func (s *Server) wireExternalRecords(src *verificationassembly.ControllerSources) {
	src.Questions = s.questionRuntime.Observations()
	if s.approvals != nil {
		src.Approvals = approvalfacts.Backfill(s.approvals.Observations)
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

// runFailure adapts delivery observations to the CI failure projection.
func runFailure(failed []delivery.FailedTask) (tasks, tests []string, class string) {
	var observations []cirunview.FailedTask
	for _, f := range failed {
		observations = append(observations, cirunview.FailedTask{Task: f.Task, Tests: f.Tests, NamesTests: f.NamesTests})
	}
	return cirunview.Failure(observations)
}
