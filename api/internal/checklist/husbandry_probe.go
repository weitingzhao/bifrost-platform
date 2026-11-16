package checklist

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry/checklistview"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/datahusbandry"
)

// HusbandrySource probes GET /api/v1/data-husbandry lanes (injected from server).
type HusbandrySource interface {
	Snapshot(ctx context.Context) datahusbandry.Snapshot
}

func (h *Handler) BindHusbandry(src HusbandrySource) { h.husbandry = src }

// ProbeHusbandrySignals maps Market/Flex/Research husbandry lanes → checklist items.
// Market due/draining → ok (not Operate). missed/degraded → fail.
// Flex source=none / degraded → fail. Batch missed/degraded via research_olap → fail.
func ProbeHusbandrySignals(snap datahusbandry.Snapshot) []ItemSignal {
	projected := checklistview.Project(snap)
	out := make([]ItemSignal, 0, len(projected))
	for _, s := range projected {
		out = append(out, ItemSignal{ItemID: s.ItemID, Signal: s.Signal, Detail: s.Detail, Env: s.Env})
	}
	return out
}

func mergeHusbandryOverlay(base []ItemSignal, overlay []ItemSignal) []ItemSignal {
	byID := map[string]ItemSignal{}
	for _, s := range base {
		byID[s.ItemID] = s
	}
	for _, s := range overlay {
		byID[s.ItemID] = s
	}
	out := make([]ItemSignal, 0, len(byID))
	for _, s := range byID {
		out = append(out, s)
	}
	return out
}

func (h *Handler) liveHusbandrySignals(ctx context.Context) []ItemSignal {
	if h.husbandry == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	sigs := ProbeHusbandrySignals(h.husbandry.Snapshot(ctx))
	// Read live on every call: observed now, by data-husbandry (TD-253).
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range sigs {
		sigs[i].ObservedAt = now
		sigs[i].Source = "data-husbandry"
	}
	return sigs
}
