package headrelease

import "context"

// What mode on does with a "would release" (prod_shadow.go says why it
// exists). This file is the loop's one act, kept apart so that
// prod_shadow.go can still be read for names that act and found to have none.

// prodAct is what was done: released (run names the delivery), waiting (why
// says for what) or error; zero when nothing was asked.
type prodAct struct {
	result, run, why string
}

const (
	ProdReleased = "released"
	ProdWaiting  = "waiting"
	ProdError    = "error"
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
