package planstore

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

func original_cardBody(root, reason string, changes []PlanChange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "A thread proposes to change plan %s.\n\nWhy: %s\n\nChanges:\n", root, reason)
	for _, c := range changes {
		raw, _ := json.Marshal(c)
		fmt.Fprintf(&b, "- %s\n", raw)
	}
	b.WriteString("\nApplying it writes the change under your decision; not applying it leaves the plan as it is.")
	return b.String()
}
func original_importCardBody(w actor.Writer, cardKey, reason string, req planimport.Request, reqID string) string {
	if req.Root.Kind == "arch_root" {
		return original_archCardBody(w, cardKey, reason, req, reqID)
	}
	items, done, active, planned := 0, 0, 0, 0
	for _, n := range req.Nodes {
		switch {
		case n.Kind == "item":
			items++
		case n.State == planimport.StateDone:
			done++
		case n.State == planimport.StateActive:
			active++
		default:
			planned++
		}
	}
	s := req.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import plan %s (%s), proposal %s.\n\nWhy: %s\n\n", w.Actor, req.Root.ID, req.Root.Title, cardKey[:12], reason)
	fmt.Fprintf(&b, "%d items, %d steps: %d done (each with its ledger line as evidence; Progress counts them as manual), %d in progress, %d not started.\n\n",
		items, done+active+planned, done, active, planned)
	fmt.Fprintf(&b, "From %s, %s, lines %d-%d, checked at %s", s.File, s.Section, s.FirstLine, s.LastLine, s.CheckedAt)
	if s.Commit != "" {
		fmt.Fprintf(&b, ", commit %s", s.Commit)
	}
	fmt.Fprintf(&b, ".\nSection sha256 %s; request %s.\n\n", s.SHA256, reqID)
	b.WriteString("Importing writes the plan under your decision; its nodes and numbers are then fixed and can only change by plan changes. " +
		"Not importing writes nothing; the board keeps reading the ledger.")
	return b.String()
}
func original_archCardBody(w actor.Writer, cardKey, reason string, req planimport.Request, reqID string) string {
	ruled, noted := 0, 0
	for _, n := range req.Nodes {
		if len(n.Rule) > 0 {
			ruled++
		}
		if planimport.ArchNow(n.Evidence.Line) != "" {
			noted++
		}
	}
	s := req.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import the architecture table %s (%s), proposal %s.\n\nWhy: %s\n\n", w.Actor, req.Root.ID, req.Root.Title,
		cardKey[:12], reason)
	fmt.Fprintf(&b, "%d rows: %d with gate tests, ticked by whether those tests passed for what is deployed now; %d with none yet, shown unknown (no gate test yet). "+
		"%d rows get what their line says they are now as their first note.\n\n", len(req.Nodes), ruled, len(req.Nodes)-ruled, noted)
	fmt.Fprintf(&b, "From %s, %s, lines %d-%d, checked at %s", s.File, s.Section, s.FirstLine, s.LastLine, s.CheckedAt)
	if s.Commit != "" {
		fmt.Fprintf(&b, ", commit %s", s.Commit)
	}
	fmt.Fprintf(&b, ".\nSection sha256 %s; request %s.\n\n", s.SHA256, reqID)
	b.WriteString("Importing writes the table under your decision; its rows and numbers are then fixed, their rules change only by plan changes, " +
		"and their notes are added to, never changed. Not importing writes nothing; the board keeps reading the ledger.")
	return b.String()
}

func TestCardDisplaysFrozenParity(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"", "why\nsecond", "<>&中文"} {
		for _, changes := range [][]PlanChange{nil, {}, {{Op: "retitle", Node: "n", Title: "text"}}, {{Op: "add_node", Parent: "p"}, {Op: "move_task", Node: "n"}}} {
			if got, want := cardBody("root", reason, changes), original_cardBody("root", reason, changes); got != want {
				t.Fatalf("change card differs: %q / %q", got, want)
			}
		}
		for _, kind := range []string{"", "root", "arch_root"} {
			for _, commit := range []string{"", "abcdef"} {
				req := planimport.Request{Root: planimport.Node{Kind: kind, ID: "root", Title: "Title"}, Source: planimport.Source{File: "ledger", Section: "section", CheckedAt: "now", Commit: commit, FirstLine: 2, LastLine: 9, SHA256: "sha"}}
				req.Nodes = []planimport.Node{{Kind: "item", Evidence: &planimport.Evidence{}}, {Kind: "step", State: planimport.StateDone, Rule: json.RawMessage(`{}`), Evidence: &planimport.Evidence{Line: "now"}}, {Kind: "step", State: planimport.StateActive, Evidence: &planimport.Evidence{}}, {Kind: "step", Evidence: &planimport.Evidence{}}}
				w := actor.Writer{Actor: "actor"}
				key := "0123456789abcdef"
				if got, want := importCardBody(w, key, reason, req, "id"), original_importCardBody(w, key, reason, req, "id"); got != want {
					t.Fatalf("import differs: %q / %q", got, want)
				}
			}
		}
	}
}
