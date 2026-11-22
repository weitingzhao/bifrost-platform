package headrelease

import (
	"context"
	"strings"
)

// What mode on does with a "would release" (prod_shadow.go says why it
// exists). This file is the loop's one act, kept apart so that
// prod_shadow.go can still be read for names that act and found to have none.

// prodAct is what was done: released (run names the delivery), waiting (why
// says for what) or error; zero when nothing was asked.
type prodAct struct {
	result, run, why string
}

const (
	// ProdFullCIStarted: the candidate waited only on its full CI, and the
	// loop started that run (fullCIFor).
	ProdFullCIStarted = "full_ci_started"
	ProdReleased      = "released"
	ProdWaiting       = "waiting"
	ProdError         = "error"
)

// act judges the moment again and, when the second reading says the same
// release, hands it to Release. Between the first reading and the start a
// person's delivery can begin and end (the head-release loop's lesson, Step
// in headrelease.go): only the same answer twice starts anything.
func (s *ProdShadowLoop) act(ctx context.Context, d ProdDecision) prodAct {
	if s.Release == nil {
		return prodAct{result: ProdError, why: "mode on has no hand to release with"}
	}
	again := DecideProd(s.Gather(ctx))
	if !again.WouldRelease || again.From != d.From || again.To != d.To {
		return prodAct{result: ProdWaiting, why: "the second reading differs: " + again.Verdict}
	}
	run, wait, err := s.Release(ctx, d.To, d.Params)
	switch {
	case err != nil:
		return prodAct{result: ProdError, why: err.Error()}
	case wait != "":
		return prodAct{result: ProdWaiting, why: wait}
	case run == "":
		return prodAct{result: ProdError, why: "the release gave no run"}
	}
	return prodAct{result: ProdReleased, run: run, why: "would release " + short(d.To) + ", judged twice"}
}

// fullCIWaits are the conditions a candidate may still fail and have the loop
// start its full CI: the full CI itself, the policy (whose ci_succeeded wants
// a Succeeded run of the commit) and the soak, which passes with time.
var fullCIWaits = map[string]bool{CondCIStrict: true, CondPolicy: true, CondSoaked: true}

// fullCIWanted says the loop should start the candidate's full CI: in hours,
// with a candidate, its full CI has no run at all ("no verdict yet" and no
// run named), and nothing else holds the release but the conditions a full
// CI can settle. Origin: 2026-11-22, the loop had been on for a day and never
// released: the daily full CI tests one commit at 07:30, main moved after
// it, and every candidate since waited on ci_strict until a seat started its
// full CI by hand.
func fullCIWanted(d ProdDecision) bool {
	if d.WouldRelease || d.To == "" || len(d.Unknown) > 0 {
		return false
	}
	ci := false
	for _, c := range d.Failing {
		if !fullCIWaits[c] {
			return false
		}
		ci = ci || c == CondCIStrict
	}
	return ci && strings.HasSuffix(strings.TrimSpace(d.Reasons[CondCIStrict]), "no verdict yet")
}

// fullCIFor starts the candidate's full CI once per candidate, in mode on.
func (s *ProdShadowLoop) fullCIFor(ctx context.Context, d ProdDecision) prodAct {
	if s.Mode != ProdOn || s.FullCI == nil || !fullCIWanted(d) {
		return prodAct{}
	}
	s.mu.Lock()
	asked := s.fullCIAsked == d.To
	s.fullCIAsked = d.To
	s.mu.Unlock()
	if asked {
		return prodAct{}
	}
	run, err := s.FullCI(ctx, d.To)
	if err != nil {
		s.mu.Lock()
		s.fullCIAsked = "" // try again next pass
		s.mu.Unlock()
		return prodAct{result: ProdError, why: "starting the full CI of " + short(d.To) + ": " + err.Error()}
	}
	return prodAct{result: ProdFullCIStarted, run: run, why: "the candidate waited only on its full CI"}
}
