package store

import (
	"encoding/json"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
	"slices"
	"strings"
	"testing"
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
func original_importCardBody(w Writer, cardKey, reason string, req planimport.Request, reqID string) string {
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
func original_archCardBody(w Writer, cardKey, reason string, req planimport.Request, reqID string) string {
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
func original_recorderCardBody(w Writer, cardKey, reason string, log recorderimport.Log, logID string) string {
	types := log.Types()
	names := make([]string, 0, len(types))
	for t := range types {
		names = append(names, t)
	}
	slices.Sort(names)
	var counts []string
	for _, t := range names {
		counts = append(counts, fmt.Sprintf("%d %s", types[t], t))
	}
	s := log.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import the recorder log of %s, proposal %s.\n\nWhy: %s\n\n", w.Actor, s.WorkID, cardKey[:12], reason)
	fmt.Fprintf(&b, "%d events: %s. The hash chain holds from the first line to the last.", len(log.Records), strings.Join(counts, ", "))
	if n := log.Masked(); n > 0 {
		fmt.Fprintf(&b, " %d of them had a path into one machine, masked before they are kept; each keeps the digest of its original line.", n)
	}
	fmt.Fprintf(&b, "\n\nFrom %s, sha256 %s; kept log %s.\n\n", s.File, s.SHA256, logID)
	b.WriteString("Importing writes the log as the stream " + RecorderStream(s.WorkID) + ", one event per line, under your decision; " +
		"it is then fixed. Not importing writes nothing; the recorder keeps its log as it is.")
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
				w := Writer{Actor: "actor"}
				key := "0123456789abcdef"
				if got, want := importCardBody(w, key, reason, req, "id"), original_importCardBody(w, key, reason, req, "id"); got != want {
					t.Fatalf("import differs: %q / %q", got, want)
				}
			}
		}
		for _, records := range [][]recorderimport.Record{nil, {{Type: "z"}, {Type: "a", Masked: true}, {Type: "z"}}} {
			log := recorderimport.Log{Source: recorderimport.Source{WorkID: "W-1", File: "log", SHA256: "hash"}, Records: records}
			w := Writer{Actor: "actor"}
			if got, want := recorderCardBody(w, "0123456789abcdef", reason, log, "id"), original_recorderCardBody(w, "0123456789abcdef", reason, log, "id"); got != want {
				t.Fatalf("recorder differs: %q / %q", got, want)
			}
		}
	}
}
