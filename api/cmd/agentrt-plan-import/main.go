// Command agentrt-plan-import turns a round of the work ledger (WORK.md,
// "第 N 轮逐项") into a plan import request, and compares a plan read from the
// runtime with the ledger (W-31, CUT-plan-import-2026-10-19.md). It reads and
// writes files only: it never sends the request anywhere.
//
//	agentrt-plan-import generate -ledger WORK.md [-round 2] [-file name] [-commit sha] [-o request.json]
//	agentrt-plan-import diff -ledger WORK.md -plan plan.json [-board board.json] [-round 2]
//
// plan.json is an answer of GET /api/v1/agentrt/plan?root=R<round>. diff
// compares it with the ledger as this command reads it and, with -board, with
// the board's own reading of the ledger (roadmap.round_of's items and steps
// as planimport.View JSON, CUT §5), an independent check of the parser. It
// prints each difference and exits 1 when there is any. A ★ the plan has and
// the board does not (after 「（补强…）」) is printed as a note, not a
// difference.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

const defaultFile = "bifrost-trade-infra/agent-config/WORK.md"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		say(stderr, "usage: agentrt-plan-import generate|diff [flags]")
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	ledger := fs.String("ledger", "", "path of the work ledger (WORK.md)")
	round := fs.Int("round", 2, "the round to read")
	file := fs.String("file", defaultFile, "the ledger's name in the request")
	commit := fs.String("commit", "", "the 40-character commit the ledger was read at")
	out := fs.String("o", "", "generate: write the request here instead of to standard output")
	planFile := fs.String("plan", "", "diff: a GET /plan?root= answer")
	boardFile := fs.String("board", "", "diff: the board's reading of the ledger, as planimport.View JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *ledger == "" {
		say(stderr, "-ledger is required")
		return 2
	}
	text, err := os.ReadFile(*ledger)
	if err != nil {
		say(stderr, err)
		return 1
	}
	req, err := planimport.ParseLedger(string(text), *round, planimport.LedgerOptions{File: *file, Commit: *commit})
	if err != nil {
		sayf(stderr, "%s: %v\n", *ledger, err)
		return 1
	}
	switch args[0] {
	case "generate":
		return generate(req, *out, stdout, stderr)
	case "diff":
		if *planFile == "" {
			say(stderr, "-plan is required")
			return 2
		}
		return diff(req, *planFile, *boardFile, stdout, stderr)
	}
	sayf(stderr, "unknown command %q: generate or diff\n", args[0])
	return 2
}

func generate(req planimport.Request, out string, stdout, stderr io.Writer) int {
	doc, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		say(stderr, err)
		return 1
	}
	doc = append(doc, '\n')
	if out == "" {
		_, err = stdout.Write(doc)
	} else {
		err = os.WriteFile(out, doc, 0o644)
	}
	if err != nil {
		say(stderr, err)
		return 1
	}
	done, active, planned := 0, 0, 0
	for _, n := range req.Nodes {
		switch n.State {
		case planimport.StateDone:
			done++
		case planimport.StateActive:
			active++
		case planimport.StatePlanned:
			planned++
		}
	}
	sayf(stderr, "%s: %d items, %d steps (%d done, %d active, %d planned), lines %d-%d, section sha256 %s\n",
		req.Root.ID, len(req.Nodes)-done-active-planned, done+active+planned, done, active, planned,
		req.Source.FirstLine, req.Source.LastLine, req.Source.SHA256)
	return 0
}

func diff(req planimport.Request, planFile, boardFile string, stdout, stderr io.Writer) int {
	doc, err := os.ReadFile(planFile)
	if err != nil {
		say(stderr, err)
		return 1
	}
	got, err := planimport.FromPlan(doc, req.Root.ID)
	if err != nil {
		sayf(stderr, "%s: %v\n", planFile, err)
		return 1
	}
	lines := planimport.Diff(planimport.FromRequest(req), got)
	if boardFile != "" {
		raw, err := os.ReadFile(boardFile)
		if err != nil {
			say(stderr, err)
			return 1
		}
		var board planimport.View
		if err := json.Unmarshal(raw, &board); err != nil {
			sayf(stderr, "%s: %v\n", boardFile, err)
			return 1
		}
		for _, n := range planimport.BoardMissesStars(board, &got) {
			say(stdout, "note: step "+n+" is a ★ step; the board does not see the ★ after 「（补强…）」")
		}
		for _, l := range planimport.Diff(board, got) {
			lines = append(lines, "against the board: "+l)
		}
	}
	for _, l := range lines {
		say(stdout, l)
	}
	if len(lines) > 0 {
		sayf(stderr, "%d differences\n", len(lines))
		return 1
	}
	say(stderr, "no differences")
	return 0
}

// say and sayf write a line to the user; a write that fails has nowhere
// better to be told.
func say(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

func sayf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
