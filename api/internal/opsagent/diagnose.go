package opsagent

import "strings"

func Diagnose(payload alertmanagerPayload) DiagnosisResponse {
	l1 := []SuggestedAction{}
	l2 := []SuggestedAction{}
	summaries := []string{}

	for _, a := range payload.Alerts {
		name := alertName(a.Labels)
		ns := labelVal(a.Labels, "namespace")
		pod := labelVal(a.Labels, "pod")
		deploy := labelVal(a.Labels, "deployment")
		node := labelVal(a.Labels, "node")

		switch {
		case strings.Contains(name, "crashloop") || strings.Contains(name, "podcrash"):
			target := deploy
			if target == "" {
				target = pod
			}
			l1 = append(l1, SuggestedAction{
				Tool:   "rollout_restart_deployment",
				Reason: "CrashLoop — restart workload in " + ns + " (" + target + ")",
				Level:  "L1",
			})
			summaries = append(summaries, "CrashLoop in "+ns+"/"+target)
		case strings.Contains(name, "replica") || strings.Contains(name, "podnotready"):
			l1 = append(l1, SuggestedAction{
				Tool:   "get_cluster_workloads",
				Reason: "Replica mismatch — inspect workloads in " + ns,
				Level:  "L0",
			})
			if pod != "" {
				l1 = append(l1, SuggestedAction{
					Tool:   "delete_pod",
					Reason: "Stale pod " + pod + " in " + ns,
					Level:  "L1",
				})
			}
			summaries = append(summaries, "Replica/pod readiness in "+ns)
		case strings.Contains(name, "nodenotready") || strings.Contains(name, "node"):
			l2 = append(l2, SuggestedAction{
				Tool:   "drain_node",
				Reason: "Node issue — drain " + node + " after Owner confirm",
				Level:  "L2",
			})
			summaries = append(summaries, "Node alert: "+node)
		default:
			l1 = append(l1, SuggestedAction{
				Tool:   "get_ops_context",
				Reason: "Generic alert " + labelVal(a.Labels, "alertname") + " — gather spine context",
				Level:  "L0",
			})
			summaries = append(summaries, labelVal(a.Labels, "alertname"))
		}
	}

	summary := strings.Join(summaries, "; ")
	if summary == "" {
		summary = payload.Status + " (" + payload.Receiver + ")"
	}

	return DiagnosisResponse{
		OK:          true,
		AlertStatus: payload.Status,
		AlertCount:  len(payload.Alerts),
		Summary:     summary,
		SuggestedL1: dedupeActions(l1),
		SuggestedL2: dedupeActions(l2),
		CursorSDKHint: "Open Cursor Ops mode Agent session; invoke MCP platform tools for suggested L1 actions; confirm L2 in Console",
		MCPPlatformTools: []string{
			"get_cluster_summary",
			"get_cluster_workloads",
			"rollout_restart_deployment",
			"delete_pod",
			"get_audit_log",
		},
	}
}

func dedupeActions(in []SuggestedAction) []SuggestedAction {
	seen := map[string]bool{}
	out := []SuggestedAction{}
	for _, a := range in {
		key := a.Tool + a.Reason
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}
