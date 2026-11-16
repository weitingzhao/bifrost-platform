package planimport

import (
	"fmt"
	"strings"
)

// Card text only; caller retains validation, approval and writes.
func ImportCardBody(actor, cardKey, reason string, req Request, reqID string) string {
	if req.Root.Kind == "arch_root" {
		return ArchitectureCardBody(actor, cardKey, reason, req, reqID)
	}
	items, done, active, planned := 0, 0, 0, 0
	for _, n := range req.Nodes {
		switch {
		case n.Kind == "item":
			items++
		case n.State == StateDone:
			done++
		case n.State == StateActive:
			active++
		default:
			planned++
		}
	}
	s := req.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import plan %s (%s), proposal %s.\n\nWhy: %s\n\n", actor, req.Root.ID, req.Root.Title, cardKey[:12], reason)
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

func ArchitectureCardBody(actor, cardKey, reason string, req Request, reqID string) string {
	ruled, noted := 0, 0
	for _, n := range req.Nodes {
		if len(n.Rule) > 0 {
			ruled++
		}
		if ArchNow(n.Evidence.Line) != "" {
			noted++
		}
	}
	s := req.Source
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes to import the architecture table %s (%s), proposal %s.\n\nWhy: %s\n\n", actor, req.Root.ID, req.Root.Title,
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
