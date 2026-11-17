// Package checklistview interprets Tenant husbandry observations for checklist display only.
package checklistview

import (
	"strings"

	husbandry "github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry"
)

type Signal struct{ ItemID, Signal, Detail, Env string }

const (
	SignalOK       = "ok"
	SignalFail     = "fail"
	SignalUnknown  = "unknown"
	SignalDegraded = "degraded"
)

func Project(snap husbandry.Snapshot) []Signal {
	byID := map[string]husbandry.LaneView{}
	for _, l := range snap.Lanes {
		byID[l.ID] = l
	}

	out := make([]Signal, 0, 3)

	if m, ok := byID["market_batch"]; ok {
		out = append(out, Signal{
			ItemID: "market-batch-sla",
			Signal: marketHusbandrySignal(m.Verdict),
			Detail: truncate("market_batch "+m.Verdict+": "+m.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, Signal{
			ItemID: "market-batch-sla",
			Signal: SignalUnknown,
			Detail: "market_batch lane missing from data-husbandry",
			Env:    "span",
		})
	}

	if f, ok := byID["flex_batch"]; ok {
		out = append(out, Signal{
			ItemID: "flex-tokens-secret",
			Signal: flexHusbandrySignal(f.Verdict, f.Source, f.Detail),
			Detail: truncate("flex_batch "+f.Verdict+" src="+f.Source+": "+f.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, Signal{
			ItemID: "flex-tokens-secret",
			Signal: SignalUnknown,
			Detail: "flex_batch lane missing from data-husbandry",
			Env:    "span",
		})
	}

	if r, ok := byID["research_olap"]; ok {
		out = append(out, Signal{
			ItemID: "research-batch-sla",
			Signal: researchHusbandrySignal(r.Verdict),
			Detail: truncate("research_olap "+r.Verdict+": "+r.Detail, 240),
			Env:    "span",
		})
	} else {
		out = append(out, Signal{
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

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
