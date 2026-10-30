package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Serial: PG and event clock. A foreign holder appears between the committed
// debit and the transport's last permit check, then an external run covers R.
func TestB5HCrossPGLostLeaseThenExternal(t *testing.T) {
	x := b5hFixture(t)
	var foreign delivery.STGWindow
	x.runs.beforeSend = func() {
		x.windows.mu.Lock()
		defer x.windows.mu.Unlock()
		foreign = x.windows.w
		foreign.Who = "manual-release"
		foreign.Generation++
		foreign.ResourceVersion = "foreign-version"
		x.windows.w = foreign
	}
	if err := x.c.Pass(ctx); err == nil {
		t.Fatal("lease takeover did not stop send")
	}
	x.runs.beforeSend = nil
	// The holder detects loss synchronously at the final send check. Drain
	// its notification through the controller before collecting new evidence.
	ok(t, x.c.Pass(ctx))
	s := x.state(t)
	lost := 0
	for _, e := range s.Records {
		if e.Record.String("kind") == "diag" && e.Record.String("what") == "lease_lost" {
			lost++
			if e.Record.Int("gen") != foreign.Generation-1 || e.Record.Int("try") != 1 {
				t.Fatal("lease loss identity changed", e)
			}
		}
	}
	if lost != 1 || x.runs.creates != 0 || b5hCount(s, "started") != 1 || !s.Batches[b5Batch].Tries[1].Occupied() {
		t.Fatal("lost lease duplicated a send or freed the slot")
	}
	external := b5hObservation(t, delivery.NamedDeliverInput{Batch: 901, Try: 1, IntentSeq: 1, P: x.p, U: commitA, Infra: commitA}, "Succeeded", "external-after-loss", "d", "f")
	x.runs.observations[external.Identity.UID] = external
	x.runs.external = []*unstructured.Unstructured{external.Run}
	x.runs.work = b5hWork("d", "f")
	ok(t, x.c.ExternalPass(ctx))
	x.advance(t, 3*time.Minute)
	x.restart(t)
	for range 3 {
		ok(t, x.c.Pass(ctx))
		ok(t, x.c.ExternalPass(ctx))
	}
	s = x.state(t)
	tr := s.Batches[b5Batch].Tries[1]
	if !tr.IsQuarantined() || !tr.Occupied() || tr.Verify.Seq != 0 || b5hCount(s, "settled") != 0 || b5hCount(s, "member") != 0 || b5hCount(s, "started") != 1 || len(s.Covers) != 1 || x.runs.creates != 0 || len(x.runs.uids) != 0 || s.Budget[store.StgBudgetKey{R: x.p, Gen: 1}] != 1 {
		t.Fatal("external cover erased unknown execution or refunded debit")
	}
	w, err := x.windows.ReadSTGWindow(ctx)
	ok(t, err)
	if !reflect.DeepEqual(w, foreign) {
		t.Fatal("old holder overwrote foreign lease", w)
	}
}

// Serial: change the authoritative rule during the external Git read, while
// the controller's immutable local policy is still stale and enabled.
func TestB5HCrossPGNoneAfterGateRead(t *testing.T) {
	x := b5hFixture(t)
	before := x.state(t)
	reads := 0
	x.c.Sources.Range = func(context.Context, string, string) ([]landing.ReleaseCommit, error) {
		reads++
		(&b5Fixture{w: x.w}).none(t)
		return x.commits, nil
	}
	ok(t, x.c.Pass(ctx))
	after := x.state(t)
	if reads != 1 || after.LastSeq != before.LastSeq || b5hCount(after, "started") != 0 || x.runs.creates != 0 || len(after.Budget) != 0 || after.Batches[b5Batch].Phase != "SNAP" {
		t.Fatal("stale local policy crossed the final authoritative none gate")
	}
	// Once the supervisor supplies none, restart does no further source I/O.
	p := *x.policy.Load()
	rule, err := store.StgControlRule(ctx, x.w.rt, "dev")
	ok(t, err)
	p.Rule = rule
	x.policy.Store(&p)
	for range 3 {
		ok(t, x.c.ExternalPass(ctx))
		ok(t, x.c.Pass(ctx))
	}
	if x.state(t).LastSeq != after.LastSeq || reads != 1 {
		t.Fatal("none appended records or read external sources")
	}
}
