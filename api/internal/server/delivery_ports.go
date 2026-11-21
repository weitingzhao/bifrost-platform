package server

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
)

// The delivery domain reaches verification's readings of CI runs (cirunview)
// and of unit plans (unitexec) through ports it declares
// (delivery/verification_ports.go); this binds them. Until 2026-11-21
// delivery imported both packages.
//
// The binding is an init of this package and not a line of New: it holds
// functions only, the same for every server, and the server's tests build
// delivery services of their own (delivery.NewHandlerForTest).
func init() { bindDeliveryVerification() }

func bindDeliveryVerification() {
	delivery.BindCIRunReadings(delivery.CIRunReadings{
		TaskRunState: cirunview.TaskRunState,
		PipelineTask: cirunview.PipelineTask,
		RunObservation: func(obj unstructured.Unstructured, pipelineName string) delivery.PipelineRunView {
			return delivery.PipelineRunView(cirunview.PipelineRunObservation(obj, pipelineName))
		},
		RunStartedAt: func(view delivery.PipelineRunView) int64 {
			return cirunview.PipelineRunStartedAt(cirunview.PipelineObservation(view))
		},
		TaskRunFacts: func(obj unstructured.Unstructured, run string) delivery.TaskRunFacts {
			t := cirunview.TaskTiming(obj, run)
			return delivery.TaskRunFacts{Name: t.Name, Task: t.Task, Status: t.Status, Finished: t.Finished, Results: fmt.Sprint(t.Results), Steps: len(t.Steps)}
		},
		ReadRunSteps: func(ctx context.Context, client func() (dynamic.Interface, error), clusterID, namespace, runName string, tasks delivery.TaskRead) delivery.PipelineRunStepsResponse {
			return cirunview.ReadRunStepsVia(ctx, client, clusterID, namespace, runName, cirunview.TaskRead(tasks))
		},
		UnitTaskRuns: cirunview.UnitTaskRuns,
		NewTimings:   func() delivery.TimingsReader { return &deliveryTimings{} },
	})
	delivery.BindUnitTaskReadings(delivery.UnitTaskReadings{
		PlanOfSelector: unitexec.PlanOfSelector,
		CompareKey:     unitexec.CompareKey,
	})
}

// deliveryTimings is cirunview's timings cache behind delivery.TimingsReader.
type deliveryTimings struct{ cache cirunview.TimingsCache }

func (t *deliveryTimings) Serve(w http.ResponseWriter, r *http.Request, pipeline string, src delivery.TimingsSource) {
	t.cache.Serve(w, r, pipeline, cirunview.TimingsSource(src))
}
