package server

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
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
func init() {
	bindDeliveryVerification()
	bindDeliveryPlacement()
	// The release window queue's thread addresses are the coordination
	// domain's (threadinbox); delivery declares the port (window_queue.go).
	delivery.SetThreadRecipient(threadinbox.Recipient)
	// The approvals domain's action runner; delivery declares the port
	// (approvals_ports.go). Until 2026-11-22 delivery imported actions.
	delivery.BindActionRuns(delivery.ActionRuns{
		AttemptName: func(ctx context.Context, base string) (string, bool) {
			att, ok := actions.CreateAttemptFrom(ctx)
			if !ok {
				return "", false
			}
			return actions.ObjectName(base, att), true
		},
		IdentityAnnotations: actions.IdentityAnnotations,
		ObjectExists:        actions.ObjectExists,
		ClassifyCreate:      actions.ClassifyCreate,
		IsUncertain:         actions.IsUncertain,
		NotRecordedHeader:   actions.NotRecordedHeader,
	})
}

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

// bindDeliveryPlacement binds compute's node placement (placement) to the
// port delivery declares (delivery/placement_ports.go): the label of each
// node role delivery names, the choice, and the build pool check. Until
// 2026-11-21 delivery imported placement and named its label rules.
func bindDeliveryPlacement() {
	delivery.BindPlacementReadings(delivery.PlacementReadings{
		HostIDLabel: placement.HostIDLabel,
		RoleLabel: func(role delivery.NodeRole) (string, string, bool) {
			r, ok := deliveryNodeRoles[role]
			return r.Key, r.Value, ok
		},
		Place: func(ctx context.Context, clientset func() (kubernetes.Interface, error), metrics delivery.NodeMetricsReader, task delivery.TaskProfile, current string) (delivery.BuildPlacement, bool) {
			t, err := placementTask(task)
			if err != nil {
				if metrics == nil {
					return delivery.BuildPlacement{}, false
				}
				return delivery.BuildPlacement{Why: "not placed: " + err.Error()}, true
			}
			var read placement.NodeMetricsReader
			if metrics != nil {
				read = placementNodeMetrics(metrics)
			}
			p, placed := placement.Placer{Clientset: clientset, Metrics: read}.Place(ctx, t, current)
			return delivery.BuildPlacement(p), placed
		},
		CIPreflightReason: func(nodes []delivery.NodeInput) string {
			in := make([]placement.NodeInput, len(nodes))
			for i, n := range nodes {
				in[i] = placement.NodeInput{Name: n.Name, Architecture: n.Architecture, WorkloadLabel: n.WorkloadLabel, Reachability: n.Reachability}
			}
			return placement.CIPreflightReason(in)
		},
	})
}

// deliveryNodeRoles are the labels of the node roles delivery names.
var deliveryNodeRoles = map[delivery.NodeRole]placement.LabelRule{
	delivery.NodeEtcd:            placement.EtcdNode,
	delivery.NodeDatabasePrimary: placement.PostgresPrimary,
}

// placementTask is delivery's task in placement's words; a role with no
// label is an error, never a rule dropped.
func placementTask(t delivery.TaskProfile) (placement.TaskProfile, error) {
	out := placement.TaskProfile{DiskHeavy: t.DiskHeavy, MaxDiskTier: t.MaxDiskTier, CPUCores: t.CPUCores, MemoryBytes: t.MemoryBytes,
		MinAvailableMemory: t.MinAvailableMemory, Spread: t.Spread}
	for _, role := range t.ExcludeNodes {
		r, ok := deliveryNodeRoles[role]
		if !ok {
			return out, fmt.Errorf("node role %q has no label", role)
		}
		out.ExcludeLabels = append(out.ExcludeLabels, r)
	}
	return out, nil
}

// placementNodeMetrics reads delivery's metrics reader as placement's.
func placementNodeMetrics(read delivery.NodeMetricsReader) placement.NodeMetricsReader {
	return func(ctx context.Context) (map[string]placement.NodeMetrics, error) {
		m, err := read(ctx)
		if m == nil {
			return nil, err
		}
		out := make(map[string]placement.NodeMetrics, len(m))
		for k, v := range m {
			out[k] = placement.NodeMetrics(v)
		}
		return out, err
	}
}

// deliveryNodeMetrics reads placement's metrics reader (the server's
// Prometheus reading) as delivery's.
func deliveryNodeMetrics(read placement.NodeMetricsReader) delivery.NodeMetricsReader {
	return func(ctx context.Context) (map[string]delivery.NodeMetrics, error) {
		m, err := read(ctx)
		if m == nil {
			return nil, err
		}
		out := make(map[string]delivery.NodeMetrics, len(m))
		for k, v := range m {
			out[k] = delivery.NodeMetrics(v)
		}
		return out, err
	}
}
