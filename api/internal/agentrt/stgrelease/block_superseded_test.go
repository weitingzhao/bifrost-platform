package stgrelease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// supersedeFixture: controlFixture's trusted baseline only, and a why block
// of P a opened against it (no batch holds Pass).
func supersedeFixture(t *testing.T, why string) (*Controller, *memoryLedger, string) {
	t.Helper()
	c, l, _, _ := controlFixture(t, "Succeeded")
	l.entries = l.entries[:1]
	block := openBlock(t, l, why)
	return c, l, block
}

// 2026-11-06 PROD: B5 stayed blocked on ci_results of P while main moved on
// and STG was released by hand. A Succeeded STG release recorded after the
// block that ran P2, a later main head (so it contains P1), lifts the block
// as superseded; the next batch selects and evaluates P2.
func TestBlockSupersededByReleaseOfNewerP(t *testing.T) {
	t.Parallel()
	c, l, block := supersedeFixture(t, "ci_results")
	c.Sources.CI = func(context.Context, string) (store.StgCIObservation, error) {
		return store.StgCIObservation{Status: "Unknown", Reason: "ci_results"}, nil
	}
	l.clock.Step(time.Minute)
	pass(t, c)
	if !l.state(t).Blocks[block].IsOpen() {
		t.Fatal("block lifted with nothing newer")
	}
	p2 := sha("f")
	at := l.clock.Now().Format(time.RFC3339Nano)
	l.add(t, store.StgRecord{"v": "1", "scope": "ext", "kind": "evidence", "evidence": "cluster/cicd/manual-2", "source": "external", "run": "manual-2", "P": p2, "U": sha("b"), "infra": sha("c"),
		"content": map[string]any{"api_digest": dig("d"), "console_digest": dig("f")}, "deployed": map[string]any{"pin_sync_ok": true, "workloads": workloads(dig("d"), dig("f")), "at": at}, "health": map[string]any{"status": "ok"}})
	pass(t, c)
	if !l.state(t).Blocks[block].IsOpen() {
		t.Fatal("lifted by a release of a commit not known to contain P")
	}
	l.effective(t, controlMember, p2) // main's head read after the block: P2
	pass(t, c)
	b := l.state(t).Blocks[block]
	if b.IsOpen() || b.Closed.Record.String("why") != "superseded" || b.Closed.Record.String("ref") != "cluster/cicd/manual-2" {
		t.Fatal("block", b.Closed.Record)
	}

	// The next batch evaluates P2, not the blocked P.
	m := memberValue(controlMember)
	m["gen"] = 1
	l.add(t, batchRecord("opened", leaseBatch, 0, "members", []any{m}, "first_pending_at", map[string]any{controlMember.R: at}))
	c.Sources.Head = func(context.Context) (string, error) { return p2, nil }
	pass(t, c)
	if got := l.state(t).Batches[leaseBatch].Selected.Record.String("P"); got != p2 {
		t.Fatal("selected", got)
	}
}

// A recorded main head past P alone lifts a ci_* block as superseded.
func TestBlockSupersededByNewerHead(t *testing.T) {
	t.Parallel()
	c, l, block := supersedeFixture(t, "ci_red")
	l.clock.Step(time.Minute)
	l.effective(t, controlMember, sha("a")) // P itself proves nothing
	pass(t, c)
	if !l.state(t).Blocks[block].IsOpen() {
		t.Fatal("lifted by a read of P itself")
	}
	l.effective(t, controlMember, sha("f"))
	pass(t, c)
	if b := l.state(t).Blocks[block]; b.IsOpen() || b.Closed.Record.String("why") != "superseded" {
		t.Fatal("block", b.Closed.Record)
	}
}

// The CI of P that cannot be read is retried for one interval, then the
// block lifts with ci_unreadable and the record keeps the error.
func TestBlockCIUnreadableLiftsAfterOneInterval(t *testing.T) {
	t.Parallel()
	for name, read := range map[string]func(context.Context, string) (store.StgCIObservation, error){
		"results": func(context.Context, string) (store.StgCIObservation, error) {
			return store.StgCIObservation{Status: "Unknown", Reason: "ci_results"}, nil
		},
		"read error": func(context.Context, string) (store.StgCIObservation, error) {
			return store.StgCIObservation{}, errors.New("tekton list failed")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, l, block := supersedeFixture(t, "ci_results")
			c.Sources.CI = read
			l.clock.Step(store.StgCIUnreadableAfter - time.Second)
			err := c.Pass(context.Background())
			if name == "read error" && err == nil || name == "results" && err != nil {
				t.Fatal("pass", err)
			}
			if !l.state(t).Blocks[block].IsOpen() {
				t.Fatal("lifted before one interval")
			}
			l.clock.Step(time.Second)
			pass(t, c)
			s := l.state(t)
			b := s.Blocks[block]
			if b.IsOpen() || b.Closed.Record.String("why") != "ci_unreadable" {
				t.Fatal("block", b.Closed.Record)
			}
			if proof := s.Records[b.Closed.Record.Int("ref")].Record; proof.String("kind") != "ci_unreadable" || proof.String("error") == "" {
				t.Fatal("proof", proof)
			}
		})
	}
}
