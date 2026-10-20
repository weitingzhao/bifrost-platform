package store

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// countingFacts counts the gate readings that reach the database.
type countingFacts struct {
	plan.Facts
	reads map[string]int
}

func (c *countingFacts) GatePassed(env, test, spec string) plan.Tri {
	c.reads[env+"/"+test+"/"+spec]++
	return plan.True
}

// Review 427 B2: rows that name the same gate share one reading per
// projection; each row still names it in its reason.
func TestArchGateReadOncePerProjection(t *testing.T) {
	t.Parallel()
	facts := &countingFacts{reads: map[string]int{}}
	gates := map[string]plan.Tri{}
	var rows []*explained
	for i := 0; i < 3; i++ {
		e := &explained{Facts: facts, gates: gates}
		e.GatePassed("stg", "T19", "s")
		e.GatePassed("prod", "T19", "s")
		rows = append(rows, e)
	}
	if facts.reads["stg/T19/s"] != 1 || facts.reads["prod/T19/s"] != 1 {
		t.Fatalf("reads = %v; want one per environment and gate", facts.reads)
	}
	for i, e := range rows {
		if len(e.atoms) != 2 || e.atoms[0].value != plan.True {
			t.Fatalf("row %d atoms = %+v", i, e.atoms)
		}
	}
}
