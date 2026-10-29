package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Serial: recorder clock used while replaying the complete legal event chain.
func TestStgLeaseLossPGIsDiagnosticOnlyAndIdempotent(t *testing.T) {
	db, conn := stgPG(t)
	ctx := context.Background()
	// An absent stream is a valid no-op; do not query its nonexistent last_seq.
	if err := RecordStgLeaseLost(ctx, conn, stgTestBatch, 1, 1); err != nil {
		t.Fatal(err)
	}
	if s := stgPGRead(t, conn); len(s.Records) != 0 {
		t.Fatal("loss created a missing batch")
	}
	h := stgReady(t, stgTestMember())
	h.add(h.start(1))
	stgPGReplay(t, conn, h.entries)
	before := stgPGRead(t, conn)
	for _, pair := range [][2]int64{{0, 1}, {1, 0}} {
		if err := RecordStgLeaseLost(ctx, conn, stgTestBatch, pair[0], pair[1]); err == nil {
			t.Fatal("invalid loss identity accepted")
		}
	}
	defer FreezeEventClock(h.at)()
	if err := RecordStgLeaseLost(ctx, conn, stgTestBatch, 1, 2); err != nil {
		t.Fatal(err)
	}
	after := stgPGRead(t, conn)
	b := after.Batches[stgTestBatch]
	if len(b.Diagnostics) != 1 || b.Phase != "STARTED" || !b.Tries[1].Occupied() || !reflect.DeepEqual(before.Budget, after.Budget) || !reflect.DeepEqual(before.Members, after.Members) {
		t.Fatal("lease loss settled execution or changed budget", b)
	}
	if err := RecordStgLeaseLost(ctx, conn, stgTestBatch, 1, 2); err != nil {
		t.Fatal("duplicate loss", err)
	}
	if err := RecordStgLeaseLost(ctx, conn, stgTestBatch, 2, 3); err != nil {
		t.Fatal("stale/wrong try notice", err)
	}
	if !reflect.DeepEqual(after, stgPGRead(t, conn)) {
		t.Fatal("duplicate or wrong try changed state")
	}
	// Two independent consumers can receive the same notice after restart.
	two := db.Connect(t, db.Runtime)
	errs := make(chan error, 2)
	for _, c := range []*pgx.Conn{conn, two} {
		go func(c *pgx.Conn) { errs <- RecordStgLeaseLost(ctx, c, stgTestBatch, 1, 3) }(c)
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := stgPGRead(t, conn); len(got.Batches[stgTestBatch].Diagnostics) != 2 || got.LastSeq != after.LastSeq+1 {
		t.Fatal("concurrent diagnostic duplicated", got.LastSeq)
	}
}

func TestStgLeaseLossNeverFinishesRunningOrUnknownExecution(t *testing.T) {
	t.Parallel()
	for _, bound := range []bool{false, true} {
		h := stgReady(t, stgTestMember())
		h.add(h.start(1))
		if bound {
			h.add(stgRecord("bound", "try", 1, "uid", "running-uid"))
		}
		before := h.state()
		h.add(stgRecord("diag", "try", 1, "what", "lease_lost", "gen", 1))
		after := h.state()
		if !after.Batches[stgTestBatch].Tries[1].Occupied() || !reflect.DeepEqual(before.Budget, after.Budget) {
			t.Fatal("lease loss freed execution")
		}
		h.reject(stgRecord("verify", "try", 1, "outcome", "failed", "why", "run_failed", "evidence", ""), "transition")
		h.reject(stgRecord("settled", "members", []any{stgTestMember()}, "why", "lease_lost"), "transition")
		// Same entry succeeds once the actual terminal evidence exists.
		if bound {
			h.add(stgRecord("run_evidence", "try", 1, "uid", "running-uid", "status", "Failed", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", h.at.Format(time.RFC3339Nano)))
		} else {
			h.terminal(1, "Failed")
		}
		verified := h.add(stgRecord("verify", "try", 1, "outcome", "failed", "why", "run_failed", "evidence", ""))
		h.add(memberRecord(stgTestMember(), "released", verified.Seq))
		h.add(stgRecord("settled", "members", []any{stgTestMember()}, "why", "run_failed"))
	}
}

func TestStgLeaseLossExternalCoverageWaitsForOriginalTerminal(t *testing.T) {
	t.Parallel()
	for _, terminalFirst := range []bool{false, true} {
		m := stgTestMember()
		h := stgReady(t, m)
		h.add(h.start(1))
		h.add(stgRecord("diag", "try", 1, "what", "lease_lost", "gen", 1))
		if terminalFirst {
			h.terminal(1, "Succeeded")
		}
		h.add(stgTestEvidence(h.at, "cluster/cicd/external-1", "external", "manual-run"))
		h.effective(m, stgSHA("b"), "effective")
		covers := stgRecord("covers")
		for k, v := range m {
			covers[k] = v
		}
		h.add(covers)
		member := memberRecord(m, "covered", "cluster/cicd/external-1")
		verify := stgRecord("verify", "try", 1, "outcome", "superseded", "why", "manual", "evidence", "cluster/cicd/external-1")
		if !terminalFirst {
			h.reject(member, "transition")
			h.reject(verify, "transition")
			h.reject(h.snapshot(2, m), "transition")
			h.reject(stgRecord("settled", "members", []any{m}, "why", "covered"), "transition")
			h.terminal(1, "Succeeded")
		}
		// Recheck deployment after the original run's terminal evidence, even
		// when the external release was discovered first.
		h.add(stgDeploymentCheck(h.at, "cluster/cicd/external-1"))
		h.add(verify)
		h.add(member)
		h.settle(m)
		s := h.state()
		if s.Batches[stgTestBatch].Phase != "CLOSED" || len(s.Covers) != 1 || s.Budget[StgBudgetKey{stgSHA("a"), 1}] != 1 {
			t.Fatal("interleaving changed classification or budget", terminalFirst)
		}
	}
}
