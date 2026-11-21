package delivery

import (
	"context"
	"log/slog"

	"k8s.io/client-go/kubernetes"
)

// Where a delivery's image builds run (the Owner, 2026-11-18: builds go to the
// fast node, and to the slower one when it is full). The choice is compute's
// (api/internal/placement, moved there 2026-11-19 at the CI line's ask); delivery
// only says what its tasks need and writes the chosen node into the run. It
// reaches compute through PlacementReadings (placement_ports.go).

// placedBuildPipelines are the pipelines whose builds are placed. They choose as
// CI does (2026-11-18: pinned to the fastest disk by its 7-day figure, builds on
// ubt-k3s-07 took 365 s against 180 while its disk answered in 799 ms).
var placedBuildPipelines = map[string]TaskProfile{
	"bifrost-deliver-platform": CITask,
}

// UnitTask is the unit runs: never on the slow disk, where two tasks timed out
// on 2026-11-18; they wait for a node instead, nothing depends on them. Never on
// the etcd node either (see CITask).
var UnitTask = TaskProfile{DiskHeavy: true, MaxDiskTier: 1, CPUCores: 4, MemoryBytes: 4 << 30, Spread: true, ExcludeNodes: []NodeRole{NodeEtcd}}

const prodPlatformPipeline = "bifrost-deliver-platform-prod"

// SetProdPrepare has fn run before a platform PROD delivery starts. It says
// nothing back: the delivery's own schema check decides what a failure means.
func (s *Service) SetProdPrepare(fn func(ctx context.Context, revision string)) { s.prepareProd = fn }

// SetProdPrepare passes to the service.
func (h *Handler) SetProdPrepare(fn func(ctx context.Context, revision string)) {
	h.svc.SetProdPrepare(fn)
}

// SetNodeMetricsReader turns build placement on; unset, runs are not placed.
func (s *Service) SetNodeMetricsReader(r NodeMetricsReader) { s.nodeMetrics = r }

// SetNodeMetricsReader passes the reader to the service.
func (h *Handler) SetNodeMetricsReader(r NodeMetricsReader) { h.svc.SetNodeMetricsReader(r) }

// placeBuild chooses the node of a placed pipeline's run and writes it into
// spec; placed says the run's annotations should record the choice.
func (s *Service) placeBuild(ctx context.Context, pipeline string, spec map[string]any) (BuildPlacement, bool) {
	task, wanted := placedBuildPipelines[pipeline]
	if !wanted {
		return BuildPlacement{}, false
	}
	p, placed := s.PlaceTask(ctx, task)
	if p.Node != "" {
		pinBuildNode(spec, p.Node)
	}
	if placed {
		slog.Info("delivery: build placement", "pipeline", pipeline, "node", p.Node, "why", p.Why)
	}
	return p, placed
}

// PlaceTask chooses the node for the task now; false when this platform reads
// no node metrics. Node "" with a reason: nothing fits or nothing could be read.
func (s *Service) PlaceTask(ctx context.Context, task TaskProfile) (BuildPlacement, bool) {
	return s.placeTask(ctx, task, "")
}

// placeTask is compute's choice; unbound, nothing is placed (as without
// node metrics).
func (s *Service) placeTask(ctx context.Context, task TaskProfile, current string) (BuildPlacement, bool) {
	place := placements().Place
	if place == nil {
		return BuildPlacement{}, false
	}
	return place(ctx, s.kubernetesClient, s.nodeMetrics, task, current)
}

func (s *Service) kubernetesClient() (kubernetes.Interface, error) {
	cs, _, err := s.cluster.KubernetesClient()
	return cs, err
}

// PlaceTask passes to the service.
func (h *Handler) PlaceTask(ctx context.Context, task TaskProfile) (BuildPlacement, bool) {
	return h.svc.PlaceTask(ctx, task)
}

// pinBuildNode adds the host to the nodeSelector of the run's podTemplate,
// keeping what the template already says.
func pinBuildNode(spec map[string]any, host string) {
	template, _ := spec["taskRunTemplate"].(map[string]any)
	if template == nil {
		template = map[string]any{}
		spec["taskRunTemplate"] = template
	}
	pod, _ := template["podTemplate"].(map[string]any)
	if pod == nil {
		pod = map[string]any{}
		template["podTemplate"] = pod
	}
	selector, _ := pod["nodeSelector"].(map[string]any)
	if selector == nil {
		selector = map[string]any{}
		pod["nodeSelector"] = selector
	}
	selector[placements().HostIDLabel] = host
}
