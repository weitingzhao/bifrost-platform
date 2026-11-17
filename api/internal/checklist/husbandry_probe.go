package checklist

import (
	"context"
	"time"
)

// SignalSource gives live item signals that a domain above the checklist
// reads from its own services. The checklist declares the shape and knows
// nothing of the domain: on 2026-11-16 this file imported the tenant's
// husbandry projection and called it itself, which made the checklist (a
// foundation package) depend on the tenant domain. The server binds the
// tenant's source (tenant/husbandry/checklistview.Source).
type SignalSource interface {
	Signals(ctx context.Context) []ItemSignal
}

func (h *Handler) BindHusbandry(src SignalSource) { h.husbandry = src }

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
	sigs := h.husbandry.Signals(ctx)
	// Read live on every call: observed now, by data-husbandry (TD-253).
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range sigs {
		sigs[i].ObservedAt = now
		sigs[i].Source = "data-husbandry"
	}
	return sigs
}
