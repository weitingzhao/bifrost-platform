package delivery

import (
	"context"
	"net/http"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// How delivery reads what the verification domain observes of CI runs (the
// reading of a PipelineRun, its TaskRuns, a run's steps and the timings
// route, cirunview) and of unit TaskRuns (unitexec). Delivery owns the
// cluster and the runs namespace and hands them over; the reading is
// verification's. The server binds the two (server/delivery_ports.go). Until
// 2026-11-21 this package imported cirunview and unitexec.

// TaskRead answers TaskRuns: from what delivery keeps, or by calling list,
// which reads the cluster.
type TaskRead func(list func() ([]unstructured.Unstructured, error)) ([]unstructured.Unstructured, error)

// TimingsSource is what delivery gives the timings read: its cluster, its
// list of a pipeline's runs, which run parameters may be shown, and its read
// of the runs' TaskRuns.
type TimingsSource struct {
	// Client is the caller's way to the cluster.
	Client func() (dynamic.Interface, error)
	// RunsOf lists the retained runs of one pipeline.
	RunsOf func(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error)
	// Params picks which of a run's parameters are shown.
	Params func(run unstructured.Unstructured) map[string]string
	// Tasks answers the TaskRuns of the runs named: from what the caller
	// keeps, or by calling list, which reads the cluster.
	Tasks func(runs []string, list func() ([]unstructured.Unstructured, error)) ([]unstructured.Unstructured, error)
}

// TimingsReader answers the timings route of one pipeline. One is made per
// handler (it keeps a short cache).
type TimingsReader interface {
	Serve(w http.ResponseWriter, r *http.Request, pipeline string, src TimingsSource)
}

// TaskRunFacts is what the side-by-side of the cached and the listed TaskRuns
// compares of one TaskRun (run_cache.go, taskKeys).
type TaskRunFacts struct {
	Name, Task, Status, Finished string
	// Results is the results as text; Steps how many steps the TaskRun has.
	Results string
	Steps   int
}

// PipelineRunStepsResponse is verification's observation of what one run
// did, relayed to the steps route as it is: delivery supplies the cluster
// and does not read the answer.
type PipelineRunStepsResponse any

// CIRunReadings is what delivery reads of runs through verification.
type CIRunReadings struct {
	// TaskRunState is "succeeded", "failed" or "running".
	TaskRunState func(obj unstructured.Unstructured) string
	// PipelineTask is the pipeline task a TaskRun ran for.
	PipelineTask func(obj unstructured.Unstructured, runName string) string
	// RunObservation is a PipelineRun as the run lists show it.
	RunObservation func(obj unstructured.Unstructured, pipelineName string) PipelineRunView
	// RunStartedAt orders the run lists, newest first.
	RunStartedAt func(view PipelineRunView) int64
	// TaskRunFacts is a TaskRun as the side-by-side compares it.
	TaskRunFacts func(obj unstructured.Unstructured, run string) TaskRunFacts
	// ReadRunSteps reads one run and its TaskRuns (through tasks).
	ReadRunSteps func(ctx context.Context, client func() (dynamic.Interface, error), clusterID, namespace, runName string, tasks TaskRead) PipelineRunStepsResponse
	// UnitTaskRuns lists the unit TaskRuns from the cluster.
	UnitTaskRuns func(ctx context.Context, dyn dynamic.Interface, namespace, label, selector string) ([]unstructured.Unstructured, error)
	// NewTimings makes the timings reader of one handler.
	NewTimings func() TimingsReader
}

// UnitTaskReadings is what delivery reads of the unit runs' plans through
// verification, for the unit cache's read-through.
type UnitTaskReadings struct {
	// PlanOfSelector is the plan a cache can answer the selector with; false
	// when only the cluster can.
	PlanOfSelector func(label, selector string) (string, bool)
	// CompareKey is what the side-by-side compares of one unit TaskRun.
	CompareKey func(name string, obj map[string]any) string
}

var (
	ciRunReadings    atomic.Pointer[CIRunReadings]
	unitTaskReadings atomic.Pointer[UnitTaskReadings]
)

// BindCIRunReadings sets the reader of runs. It is called once, at start-up,
// before anything is served. Unbound: a run's state and task are unknown, a
// run shows its name and status Unknown, the steps and timings routes answer
// 503 and the unit TaskRuns are not listed.
func BindCIRunReadings(r CIRunReadings) { ciRunReadings.Store(&r) }

// BindUnitTaskReadings sets the reader of the unit plans. Unbound: every
// unit TaskRun read goes to the cluster, none to the cache.
func BindUnitTaskReadings(r UnitTaskReadings) { unitTaskReadings.Store(&r) }

func ciRuns() CIRunReadings {
	if r := ciRunReadings.Load(); r != nil {
		return *r
	}
	return CIRunReadings{}
}

func unitTasks() UnitTaskReadings {
	if r := unitTaskReadings.Load(); r != nil {
		return *r
	}
	return UnitTaskReadings{}
}

// The answers of the routes whose reading is unbound.
const (
	errRunStepsUnbound    = "run steps are not wired on this platform (delivery.BindCIRunReadings)"
	errTimingsUnbound     = "pipeline timings are not wired on this platform (delivery.BindCIRunReadings)"
	errUnitTaskRunUnbound = "unit TaskRuns are not wired on this platform (delivery.BindCIRunReadings)"
)
