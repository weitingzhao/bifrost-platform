package stgrelease

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"testing"
)

func TestSentCINoLongerRuns(t *testing.T) {
	t.Parallel()
	c, l, _, _ := controlFixture(t, "Running")
	c.Sources.CI = func(context.Context, string) (store.StgCIObservation, error) {
		t.Fatal("after-send CI watch ran")
		return store.StgCIObservation{}, nil
	}
	before := len(l.state(t).BlockHistory)
	if err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.state(t).BlockHistory) != before {
		t.Fatal("after-send CI wrote a block")
	}
}

func openBlocks(s store.StgState) []store.StgBlock {
	var out []store.StgBlock
	for _, b := range s.Blocks {
		if b.IsOpen() {
			out = append(out, b)
		}
	}
	return out
}
