package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// Where CI runs go (the Owner, 2026-11-18: move the CI on ubt-k3s-05 to 07, the
// slow disk only as the fallback). A CI run is created by the Gitea webhook's
// trigger, not by the platform, so the platform keeps the current choice in a
// TriggerBinding the trigger reads: each new run is pinned whole to the node
// chosen at that moment, and a run already going keeps its node. CI never waits
// for the platform: with the platform away the last choice stands.

const (
	ciNodeBinding = "bifrost-ci-node"
	// Selector of a run left to the scheduler: true of every node.
	ciAnyNodeKey, ciAnyNodeValue = "kubernetes.io/os", "linux"
	ciNoNode                     = "none"
)

var triggerBindingGVR = schema.GroupVersionResource{Group: "triggers.tekton.dev", Version: "v1beta1", Resource: "triggerbindings"}

// CITask is a CI run: the least loaded node off the slow disk, the slow disk
// rather than waiting, and never ubt-k3s-01, whose etcd slowed under CI.
var CITask = TaskProfile{DiskHeavy: true, MaxDiskTier: 2, CPUCores: 4, MemoryBytes: 4 << 30, Spread: true, Exclude: []string{"ubt-k3s-01"}}

// ciRunExcludedNodes are never given a platform CI run, chosen or not: ubt-k3s-01
// (see CITask), and ubt-k3s-02, the database primary with 15 GiB, which has no memory
// for the test database a run keeps in memory since 2026-11-19 (7.46 GiB available
// at its lowest in seven days, against the 6 GiB floor).
var ciRunExcludedNodes = []string{"ubt-k3s-01", "ubt-k3s-02"}

// ciRunMemoryFloor is the compute line's room for memory volumes turned into a
// reading: a volume takes what it holds from the available memory, so a node stays
// a candidate down to the 6 GiB floor plus 1 GiB for runs starting meanwhile.
const ciRunMemoryFloor = 7 << 30

// CIRunTask is a whole CI run as the trigger starts it. Image builds hold no test
// database and stay CITask.
var CIRunTask = func() TaskProfile {
	t := CITask
	t.Exclude = ciRunExcludedNodes
	t.MinAvailableMemory = ciRunMemoryFloor
	return t
}()

// ciRunTaskRunTemplate is the pod template of a platform CI run the platform starts
// itself: kept off the same nodes as one the trigger starts.
func ciRunTaskRunTemplate() map[string]any {
	t := amd64CITaskRunTemplate()
	t["podTemplate"].(map[string]any)["affinity"] = map[string]any{"nodeAffinity": map[string]any{
		"requiredDuringSchedulingIgnoredDuringExecution": map[string]any{"nodeSelectorTerms": []any{map[string]any{
			"matchExpressions": []any{map[string]any{"key": "kubernetes.io/hostname", "operator": "NotIn", "values": slices.Clone(ciRunExcludedNodes)}},
		}}},
	}}
	return t
}

func ciNodeParams(p BuildPlacement) map[string]string {
	out := map[string]string{"ciNodeKey": hostIDLabel, "ciNodeValue": p.Node, "ciNode": p.Node, "ciNodeWhy": p.Why}
	if p.Node == "" {
		out["ciNodeKey"], out["ciNodeValue"], out["ciNode"] = ciAnyNodeKey, ciAnyNodeValue, ciNoNode
	}
	if out["ciNodeWhy"] == "" {
		out["ciNodeWhy"] = "no reason given"
	}
	return out
}

// PublishCINode writes the node for new CI runs into the binding when the choice
// changed. A binding that is not there, or that this platform may not write, is
// not an error: that is every platform but the one given it.
func (s *Service) PublishCINode(ctx context.Context) error {
	if s.nodeMetrics == nil {
		return nil
	}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return err
	}
	res := dyn.Resource(triggerBindingGVR).Namespace(s.PipelinesNamespace())
	current, err := res.Get(ctx, ciNodeBinding, metav1.GetOptions{})
	if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", ciNodeBinding, err)
	}
	p, _ := s.placeTask(ctx, CIRunTask, boundParam(current.Object, "ciNode"))
	want := ciNodeParams(p)
	if boundParam(current.Object, "ciNode") == want["ciNode"] {
		return nil
	}
	params := make([]map[string]string, 0, len(want))
	for _, name := range []string{"ciNode", "ciNodeKey", "ciNodeValue", "ciNodeWhy"} {
		params = append(params, map[string]string{"name": name, "value": want[name]})
	}
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"params": params}})
	if err != nil {
		return err
	}
	if _, err := res.Patch(ctx, ciNodeBinding, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		if apierrors.IsForbidden(err) {
			return nil
		}
		return fmt.Errorf("write %s: %w", ciNodeBinding, err)
	}
	slog.Info("delivery: CI node changed", "node", want["ciNode"], "why", want["ciNodeWhy"])
	return nil
}

// PublishCINode passes to the service.
func (h *Handler) PublishCINode(ctx context.Context) error { return h.svc.PublishCINode(ctx) }

func boundParam(obj map[string]any, name string) string {
	spec, _ := obj["spec"].(map[string]any)
	params, _ := spec["params"].([]any)
	for _, raw := range params {
		if p, _ := raw.(map[string]any); p["name"] == name {
			v, _ := p["value"].(string)
			return v
		}
	}
	return ""
}
