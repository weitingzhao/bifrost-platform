package checklistview

import (
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
	husbandry "github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry"
)

func baselineHusbandrySignals(snap husbandry.Snapshot) []checklist.ItemSignal {
	byID := map[string]husbandry.LaneView{}
	for _, l := range snap.Lanes {
		byID[l.ID] = l
	}

	out := make([]checklist.ItemSignal, 0, 3)

	if m, ok := byID["market_batch"]; ok {
		out = append(out, checklist.ItemSignal{
			ItemID: "market-batch-sla",
			Signal: baselineMarketSignal(m.Verdict),
			Detail: truncate("market_batch "+m.Verdict+": "+m.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, checklist.ItemSignal{
			ItemID: "market-batch-sla",
			Signal: SignalUnknown,
			Detail: "market_batch lane missing from data-husbandry",
			Env:    "span",
		})
	}

	if f, ok := byID["flex_batch"]; ok {
		out = append(out, checklist.ItemSignal{
			ItemID: "flex-tokens-secret",
			Signal: baselineFlexSignal(f.Verdict, f.Source, f.Detail),
			Detail: truncate("flex_batch "+f.Verdict+" src="+f.Source+": "+f.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, checklist.ItemSignal{
			ItemID: "flex-tokens-secret",
			Signal: SignalUnknown,
			Detail: "flex_batch lane missing from data-husbandry",
			Env:    "span",
		})
	}

	if r, ok := byID["research_olap"]; ok {
		out = append(out, checklist.ItemSignal{
			ItemID: "research-batch-sla",
			Signal: baselineResearchSignal(r.Verdict),
			Detail: truncate("research_olap "+r.Verdict+": "+r.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, checklist.ItemSignal{
			ItemID: "research-batch-sla",
			Signal: SignalUnknown,
			Detail: "research_olap lane missing from data-husbandry",
			Env:    "span",
		})
	}

	return out
}

func baselineMarketSignal(verdict string) string {
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

func baselineFlexSignal(verdict, source, detail string) string {
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

func baselineResearchSignal(verdict string) string {
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
