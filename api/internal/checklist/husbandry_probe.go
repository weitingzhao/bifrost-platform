package checklist

import (
	"context"
	"strings"
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
	byID := map[string]datahusbandry.LaneView{}
	for _, l := range snap.Lanes {
		byID[l.ID] = l
	}

	out := make([]ItemSignal, 0, 3)

	if m, ok := byID["market_batch"]; ok {
		out = append(out, ItemSignal{
			ItemID: "market-batch-sla",
			Signal: marketHusbandrySignal(m.Verdict),
			Detail: truncate("market_batch "+m.Verdict+": "+m.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, ItemSignal{
			ItemID: "market-batch-sla",
			Signal: SignalUnknown,
			Detail: "market_batch lane missing from data-husbandry",
			Env:    "span",
		})
	}

	if f, ok := byID["flex_batch"]; ok {
		out = append(out, ItemSignal{
			ItemID: "flex-tokens-secret",
			Signal: flexHusbandrySignal(f.Verdict, f.Source, f.Detail),
			Detail: truncate("flex_batch "+f.Verdict+" src="+f.Source+": "+f.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, ItemSignal{
			ItemID: "flex-tokens-secret",
			Signal: SignalUnknown,
			Detail: "flex_batch lane missing from data-husbandry",
			Env:    "span",
		})
	}

	if r, ok := byID["research_olap"]; ok {
		out = append(out, ItemSignal{
			ItemID: "research-batch-sla",
			Signal: researchHusbandrySignal(r.Verdict),
			Detail: truncate("research_olap "+r.Verdict+": "+r.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, ItemSignal{
			ItemID: "research-batch-sla",
			Signal: SignalUnknown,
			Detail: "research_olap lane missing from data-husbandry",
			Env:    "span",
		})
	}

	return out
}

func marketHusbandrySignal(verdict string) string {
	v := strings.ToLower(strings.TrimSpace(verdict))
	switch v {
	case "healthy", "due", "draining":
		return SignalOK
	case "missed", "degraded":
		return SignalFail
	case "caution":
		return SignalDegraded
	default:
		return SignalUnknown
	}
}

func flexHusbandrySignal(verdict, source, detail string) string {
	v := strings.ToLower(strings.TrimSpace(verdict))
	src := strings.ToLower(strings.TrimSpace(source))
	d := strings.ToLower(detail)
	if src == "none" || strings.Contains(d, "source=none") || strings.Contains(d, "token source=none") {
		return SignalFail
	}
	switch v {
	case "healthy":
		return SignalOK
	case "degraded", "missed":
		return SignalFail
	case "caution", "due", "draining":
		return SignalDegraded
	default:
		return SignalUnknown
	}
}

func researchHusbandrySignal(verdict string) string {
	v := strings.ToLower(strings.TrimSpace(verdict))
	switch v {
	case "healthy":
		return SignalOK
	case "missed", "degraded":
		return SignalFail
	case "caution", "due", "draining":
		return SignalDegraded
	default:
		return SignalUnknown
	}
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
	return ProbeHusbandrySignals(h.husbandry.Snapshot(ctx))
}
