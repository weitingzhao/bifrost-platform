package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"time"
)

func (s *planFactsSnapshot) itemValue(root, node string) plan.Tri {
	d := s.Docs[root]
	if d == nil {
		return plan.Unknown
	}
	for _, n := range d.Items {
		if n.Node == node {
			v := plan.True
			for _, m := range n.Milestones {
				mv := s.milestone(root, node, m, map[string]bool{})
				if mv == plan.False {
					return mv
				}
				if mv == plan.Unknown {
					v = mv
				}
			}
			return v
		}
	}
	return plan.Unknown
}

func verificationView(s *planFactsSnapshot, root string, nodes []map[string]any, graph planview.Graph, adoption viewAdoption, candidates map[string][]candidateEvidence, attemptedSteps map[string]bool) (map[string]any, []any, []map[string]any, []any, []any, string) {
	mainline, next, later := viewExecutionSummary(nodes, graph, adoption)
	waiting := []map[string]any{}
	ready := []any{}
	freshness := "Unknown"
	byID := map[string]map[string]any{}
	for _, n := range nodes {
		byID[n["plan_node_id"].(string)] = n
	}
	d := s.Docs[root]
	if d == nil {
		return mainline, next, waiting, ready, later, freshness
	}
	bs, _ := d.Bindings()
	hasBindings := len(bs) > 0
	ownerSteps := map[string]bool{}
	for _, b := range bs {
		if b.Source == "owner_signoff" {
			ownerSteps[b.Step] = true
		}
	}
	legacyLater := []any{}
	for _, entry := range later {
		row := entry.(map[string]any)
		id, _ := row["plan_node_id"].(string)
		if !ownerSteps[id] {
			legacyLater = append(legacyLater, entry)
		}
	}
	later = legacyLater
	statuses := map[string][]map[string]any{}
	for _, b := range bs {
		v, status, o := s.reading(b)
		effective := s.bindingValue(b, map[string]bool{})
		statuses[b.Node] = append(statuses[b.Node], map[string]any{"id": b.Acceptance, "binding": b.ID, "atom": b.Atom, "value": effective.String(), "source_value": v.String(), "freshness": status, "source_id": o.Source, "source_version": o.SourceVersion, "evidence": o.Evidence, "last_success": o.LastSuccess, "last_known_value": o.LastValue, "reason": o.Reason})
		if status == "Stale" {
			freshness = "Stale"
		} else if freshness != "Stale" && status == "Unknown" {
			freshness = "Partial"
		} else if freshness == "Unknown" {
			freshness = "Fresh"
		}
		if b.Source == "owner_signoff" && effective != plan.True {
			canSign := true
			for _, other := range bs {
				if other.Step == b.Step && other.ID != b.ID && other.Source != "owner_signoff" && s.bindingValue(other, map[string]bool{}) != plan.True {
					canSign = false
				}
			}
			for _, n := range d.Items {
				if n.Node == b.Node {
					for _, edge := range n.Dependencies {
						if edge.Relation == "blocks" && s.milestone(edge.Root, edge.Node, edge.Milestone, map[string]bool{}) != plan.True {
							canSign = false
						}
					}
				}
			}
			entry := map[string]any{"plan_node_id": b.Step, "ref": b.SignoffRef(), "acceptance": b.Acceptance, "plan_version": b.Version, "evidence": nonNil(o.Evidence), "candidate_evidence": candidates[b.Step], "reason": "Owner acceptance signoff", "source": "adopted plan binding"}
			// Use the same step state as viewExecutionSummary, plus any task
			// attempt (including ended attempts). Unknown is not proof of work.
			state, _ := byID[b.Step]["state"].(string)
			started := state == "active" || state == "running" || state == "done" || attemptedSteps[b.Step]
			if !canSign {
				entry["readiness"] = "Blocked"
				entry["reason"] = "Required acceptance conditions or dependencies are not currently verified"
				later = append(later, entry)
			} else if started || len(candidates[b.Step]) > 0 {
				entry["readiness"] = "Ready"
				ready = append(ready, entry)
			} else {
				entry["readiness"] = "Not started"
				entry["reason"] = "Work for this step has not started; no candidate evidence yet"
				later = append(later, entry)
			}
		}
	}
	for _, n := range d.Items {
		if row := byID[adoption.Nodes[n.Node]]; row != nil {
			row["acceptance_status"] = statuses[n.Node]
			row["current_validity"] = s.itemValue(root, n.Node).String()
		}
	}
	for _, edge := range graph.Edges {
		if edge.Relation != "blocks" {
			continue
		}
		v := s.milestone(edge.To.Root, edge.To.Node, edge.Milestone, map[string]bool{})
		if v == plan.True {
			continue
		}
		status := "Blocked"
		if v == plan.Unknown {
			status = "Unknown"
		}
		responsible := planview.Responsible{Kind: "owner"}
		reason := "Required milestone is not currently verified"
		if target := s.Docs[edge.To.Root]; target != nil {
			responsible = target.Items[itemIndex(target, edge.To.Node)].Responsible
		} else {
			reason = "Required plan view is unavailable; Owner must restore it"
		}
		waiting = append(waiting, map[string]any{"edge": edge, "status": status, "reason": reason, "responsible": responsible})
	}
	if hasBindings {
		path := []any{}
		next = []any{}
		var current any
		for _, ref := range graph.Mainline {
			v := s.itemValue(ref.Root, ref.Node)
			entry := map[string]any{"ref": ref, "plan_node_id": adoption.Nodes[ref.Node], "milestone_status": v.String(), "current_validity": v.String()}
			path = append(path, entry)
			if current == nil && v != plan.True {
				current = entry
				next = append(next, map[string]any{"plan_node_id": adoption.Nodes[ref.Node], "action": "Satisfy unverified acceptance conditions", "responsible": d.Items[itemIndex(d, ref.Node)].Responsible})
			}
		}
		mainline = map[string]any{"path": path, "current": current, "basis": "Versioned acceptance observations"}
	}
	return mainline, next, waiting, ready, later, freshness
}

func (s *planFactsSnapshot) taskDelivery(task string) string {
	label := "Landing unverified"
	onMain, deployFailed, deployPassed := false, false, false
	for _, d := range s.Docs {
		bs, _ := d.Bindings()
		for _, b := range bs {
			if b.Task != task {
				continue
			}
			v, _, _ := s.reading(b)
			switch b.Atom {
			case "main_effective":
				if v == plan.True {
					onMain = true
				} else if v == plan.False {
					label = "Not landed"
				}
			case "deployment_verified":
				deployFailed = deployFailed || v == plan.False
				deployPassed = deployPassed || v == plan.True
			}
		}
	}
	if onMain {
		label = "On main"
		if deployFailed {
			label = "On main · Deployment verification failed"
		}
		if deployPassed {
			label = "On main · Deployment verified"
		}
	}
	return label
}

// Landing lifecycle is folded from note.landing by the existing reducer. It
// explains waiting/delivery but cannot replace fresh Git and deployment proof.
func (s *planFactsSnapshot) taskLanding(task string) map[string]any {
	var latest time.Time
	var out map[string]any
	for stream, state := range s.Landings {
		for id, o := range state.Operations {
			if o.Task != task || !o.LastAt.After(latest) {
				continue
			}
			latest = o.LastAt
			out = map[string]any{"stream": stream, "operation": id, "phase": o.Phase, "outcome": o.Outcome, "reason": o.Why, "attempt": o.Attempt, "result": o.A, "main_commit": o.R, "run": o.Run, "observed_at": timeText(o.LastAt), "basis": "note.landing; current validity requires verification"}
		}
	}
	return out
}

func (s *planFactsSnapshot) externalVerification(id string) string {
	found, unknown, stale, failed := false, false, false, false
	for _, d := range s.Docs {
		bs, _ := d.Bindings()
		for _, b := range bs {
			if b.ExternalWork != id || b.Atom != "external_verified" {
				continue
			}
			found = true
			v, status, _ := s.reading(b)
			unknown = unknown || v == plan.Unknown
			stale = stale || status == "Stale"
			failed = failed || v == plan.False
		}
	}
	if stale {
		return "Stale"
	}
	if !found || unknown {
		return "Unknown"
	}
	if failed {
		return "Verification failed"
	}
	return "Verified"
}
