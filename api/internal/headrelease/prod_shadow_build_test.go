package headrelease

import (
	"context"
	"testing"
	"time"
)

// Origin: 2026-11-18, decoupling item 5: the shadow's switch, rows and pace
// moved here from the server, where the only test of them ran on a Server
// with no delivery service and so never reached them. Off, or without its
// rows, nothing is built and nothing is read; on (the positive control), the
// loop has its pace and its record goes to the audit under the row's name.
func TestNewProdShadowIsBuiltOnlyWhenOnAndWithItsRows(t *testing.T) {
	table := Table{Deliverables: []Deliverable{{Name: ProdRowName}, {Name: ProdSourceName}}}
	reads := 0
	type audited struct{ target, result, detail string }
	var got []audited
	hands := ProdShadowHands{
		Reads: func() ProdReads { reads++; return ProdReads{} },
		Batch: func(context.Context, string, string, string) ProdBatch { return ProdBatch{} },
		Audit: func(target, result, detail string) { got = append(got, audited{target, result, detail}) },
	}

	t.Setenv(prodShadowEnv, "off")
	if NewProdShadow(table, hands) != nil || reads != 0 {
		t.Fatalf("off built a shadow (reads %d)", reads)
	}
	t.Setenv(prodShadowEnv, ProdShadow)
	if NewProdShadow(Table{Deliverables: table.Deliverables[:1]}, hands) != nil || reads != 0 {
		t.Fatalf("a table without its source row built a shadow (reads %d)", reads)
	}
	loop := NewProdShadow(table, hands)
	if loop == nil || reads != 1 || loop.Mode != ProdShadow || loop.Every != 5*time.Minute {
		t.Fatalf("on: %+v, reads %d", loop, reads)
	}
	loop.Record("wait", "why")
	if len(got) != 1 || got[0] != (audited{"deliverable/" + ProdRowName, "wait", "why"}) {
		t.Fatalf("audited %+v", got)
	}
}
