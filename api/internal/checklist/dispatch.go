package checklist

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/operatequeue"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

const (
	dedupWindow       = 24 * time.Hour
	maxConcurrentAuto = 1
)

// GateForCapability maps fixCapability → dispatch gate.
func GateForCapability(cap FixCapability) string {
	switch cap {
	case FixFullAuto:
		return "auto"
	case FixSemiAuto:
		return "queue"
	case FixManual, FixObserve:
		return "notify"
	default:
		return "notify"
	}
}

// PlanActions builds dispatch actions without side effects (for tests + dry-run).
func PlanActions(signals []ItemSignal, recent []DispatchAction, activeAutoJobs int) []DispatchAction {
	now := time.Now().UTC()
	recentByItem := map[string]bool{}
	for _, a := range recent {
		if a.Gate != "auto" && a.Gate != "queue" {
			continue
		}
		if a.At == "" {
			recentByItem[a.ItemID] = true
			continue
		}
		at, err := time.Parse(time.RFC3339, a.At)
		if err != nil {
			recentByItem[a.ItemID] = true
			continue
		}
		if now.Sub(at) < dedupWindow {
			recentByItem[a.ItemID] = true
		}
	}

	var out []DispatchAction
	autoSlots := maxConcurrentAuto - activeAutoJobs
	if autoSlots < 0 {
		autoSlots = 0
	}
	stamp := now.Format(time.RFC3339)

	for _, sig := range signals {
		if sig.Signal != SignalFail && sig.Signal != SignalDegraded {
			continue
		}
		meta, ok := ItemByID(sig.ItemID)
		if !ok {
			continue
		}
		gate := GateForCapability(meta.FixCapability)

		// D10: IB feed never auto-dispatches
		if meta.ID == "ib-feed" || meta.FixCapability == FixObserve {
			out = append(out, DispatchAction{
				ItemID: meta.ID, Gate: "skip", FixScope: meta.FixScope, At: stamp,
				Detail: "D10 observe — never auto-dispatch IB feed", SkippedD10: true,
			})
			continue
		}

		if gate == "notify" {
			out = append(out, DispatchAction{
				ItemID: meta.ID, Gate: "notify", FixScope: meta.FixScope, At: stamp,
				Detail: fmt.Sprintf("%s — operator notify only: %s", meta.Label, truncate(sig.Detail, 160)),
			})
			continue
		}

		if recentByItem[meta.ID] {
			out = append(out, DispatchAction{
				ItemID: meta.ID, Gate: "skip", FixScope: meta.FixScope, At: stamp,
				Detail: "dedup: dispatched within last 24h window",
			})
			continue
		}

		if gate == "auto" {
			if autoSlots <= 0 {
				out = append(out, DispatchAction{
					ItemID: meta.ID, Gate: "queue", FixScope: meta.FixScope, At: stamp,
					Detail: "concurrent auto limit reached — demoted to Operate Queue",
				})
				continue
			}
			autoSlots--
			out = append(out, DispatchAction{
				ItemID: meta.ID, Gate: "auto", FixScope: meta.FixScope, At: stamp,
				Detail: truncate(sig.Detail, 200),
			})
			continue
		}

		out = append(out, DispatchAction{
			ItemID: meta.ID, Gate: "queue", FixScope: meta.FixScope, At: stamp,
			Detail: truncate(sig.Detail, 200),
		})
	}
	return out
}

func (h *Handler) executeDispatch(ctx context.Context, signals []ItemSignal) []DispatchAction {
	activeAuto := countActiveAutoJobs(h.remediation)
	planned := PlanActions(signals, h.lastDispatchSnapshot(), activeAuto)

	sigDetail := map[string]string{}
	for _, s := range signals {
		sigDetail[s.ItemID] = s.Detail
	}

	out := make([]DispatchAction, 0, len(planned))
	for _, a := range planned {
		switch a.Gate {
		case "auto":
			scope := a.FixScope
			if scope == "" {
				scope = "cluster_issues_full_auto"
			}
			prompt := buildFixPrompt(a.ItemID, sigDetail[a.ItemID])
			job, err := h.remediation.StartInternal(ctx, remediation.StartRunnerRequest{
				Scope:  scope,
				Actor:  "checklist-dispatch",
				Prompt: prompt,
			})
			if err != nil {
				a.Gate = "notify"
				a.Detail = "auto start failed: " + err.Error()
				out = append(out, a)
				continue
			}
			a.JobID = job.ID
			a.Detail = "started remediation job"
			out = append(out, a)
		case "queue":
			meta, _ := ItemByID(a.ItemID)
			agentTaskID := "daily-ops-checklist-run"
			if a.ItemID == "git-bridge" || meta.FixScope == "git-dirty-remediate" {
				agentTaskID = "git-dirty-remediate"
			}
			item, err := h.operate.EnqueueChecklistDispatch(operatequeue.EnqueueRequest{
				ProgramID:   "daily-ops-checklist",
				OperateLane: "troubleshoot",
				Title:       fmt.Sprintf("Checklist · %s", meta.Label),
				Description: fmt.Sprintf("semi_auto handoff for item %s\n\n%s", a.ItemID, sigDetail[a.ItemID]),
				HandoffKind: operatequeue.HandoffOneOff,
				Reason:      "checklist_dispatch",
				AgentTaskID: agentTaskID,
				AcceptanceCriteria: []string{
					fmt.Sprintf("Checklist item %s returns ok", a.ItemID),
				},
				VerificationSteps: []string{
					"verify_mission_snapshot",
					"Re-check Daily Ops Checklist item signal",
				},
				RiskLevel: operatequeue.RiskMedium,
				Owner:     "ops",
			})
			if err != nil {
				a.Gate = "notify"
				a.Detail = "queue enqueue failed: " + err.Error()
				out = append(out, a)
				continue
			}
			a.QueueID = item.ID
			// Preserve busy-demote reason (concurrent auto=1) for Action UI (Queued (busy)).
			if strings.Contains(strings.ToLower(a.Detail), "concurrent auto") ||
				strings.Contains(strings.ToLower(a.Detail), "demoted") {
				a.Detail = "concurrent auto limit — demoted; enqueued Operate Queue (checklist_dispatch)"
			} else {
				a.Detail = "enqueued Operate Queue (checklist_dispatch)"
			}
			out = append(out, a)
		default:
			out = append(out, a)
		}
	}
	return out
}

func buildFixPrompt(itemID, detail string) string {
	meta, ok := ItemByID(itemID)
	label := itemID
	if ok {
		label = meta.Label
	}
	playbook := ""
	switch itemID {
	case "massive-polygon":
		playbook = "Playbook: massive-feed-recover\n"
	case "market-batch-sla":
		playbook = "Playbook: Open Massive ingest/queue; clear market_batch missed/degraded (due is OK).\n"
	case "flex-tokens-secret":
		playbook = "Playbook: make sync-flex-tokens — confirm bifrost-flex-tokens non-empty; summary source=secret.\n"
	case "research-batch-sla":
		playbook = "Playbook: Open Dagster; ensure dagster-daemon Ready + research_trading_day_schedule RUNNING; inspect ops_dagster.runs.\n"
	case "postgres", "redis":
		playbook = "Playbook: data-layer-recover\n"
	case "git-bridge":
		playbook = "Playbook: git-dirty-remediate\n"
	case "runners-ha", "hermes-tooling":
		playbook = "Playbook: operator-plane-remediate\n"
	}
	return fmt.Sprintf(
		"%sChecklist auto-dispatch for item `%s` (%s).\nDetail: %s\n\nDiagnose with tools, remediate safely, then verify_mission_snapshot. D10: no live trading.",
		playbook, itemID, label, truncate(detail, 400),
	)
}

func countActiveAutoJobs(rh *remediation.Handler) int {
	if rh == nil {
		return 0
	}
	n := 0
	for _, j := range rh.Store().List() {
		if j.Status != remediation.JobRunning {
			continue
		}
		if j.Actor == "checklist-dispatch" {
			n++
		}
	}
	return n
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// DedupWindow exported for tests.
func DedupWindow() time.Duration { return dedupWindow }
