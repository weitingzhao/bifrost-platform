package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

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

// CITask is a CI run: the fastest disk first, and the slow disk rather than
// waiting (tests lose 10 to 16% there, builds more than half).
var CITask = TaskProfile{DiskHeavy: true, MaxDiskTier: 2, CPUCores: 4, MemoryBytes: 4 << 30}

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
	p, measured := s.PlaceTask(ctx, CITask)
	if !measured {
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
