package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"testing"
)

func TestStgBatchCommitProvenance(t *testing.T) {
	t.Parallel()
	x := stgPolicy(t)
	first := x.o.Commits[0]
	first.SHA = stgSHA("a")
	last := first
	last.SHA, last.Parents = stgSHA("b"), []string{first.SHA}
	last.Message = "Change\n\nAgentrt-Task: " + stgTestRoot2 + "\nLanded-By: agentrt-prod/land:two:1\n"
	commits := []landing.ReleaseCommit{first, last}
	op := "land:one:1"
	x.s.Legacy.Operations["land:two:1"] = landing.Operation{Task: stgTestRoot2, R: last.SHA, M0: stgSHA("e"), EverLanded: true}
	b := LandingBatch{Batch: landing.BatchID(op), Lead: op, Members: []string{op, "land:two:1"}, M0: stgSHA("e"), R: last.SHA, CommitterDate: x.at}
	// Exercise the production fold rather than installing the record directly.
	x.h.raw(map[string]any{"text": "batch", "landing_batch": b.body()})
	x.s.LandingBatches = x.h.state().LandingBatches
	for i := range commits {
		if !stgCommitMatchesLanding(x.s, commits, i) {
			t.Fatalf("batch member %d refused", i)
		}
	}
	x.o.Commits = commits
	if d := x.decide(false); d.Why != "binding_changed" {
		t.Fatalf("batch did not reach per-member authorization: %+v", d)
	}
	x.f.Bindings[stgTestRoot2] = stgBinding{Root: stgTestRoot2, Tenant: "dev", Repo: "bifrost-platform"}
	x.f.Authority["land:two:1"] = stgLandingAuthority{RuleVersion: 1}
	second := map[string]any{"root": stgTestRoot2, "op": "land:two:1", "R": last.SHA}
	batch := x.s.Batches[stgTestBatch]
	batch.Members[stgMember(second)] = 1
	batch.Tries[1].Snapshot.Record["set"] = []any{map[string]any{"root": stgTestRoot, "op": op, "R": last.SHA}, second}
	if d := x.decide(false); !d.Allowed || len(d.Set) != 2 || d.Set[0].R != last.SHA || d.Set[1].R != last.SHA {
		t.Fatalf("batch gate refused or lost shared landing identity: %+v", d)
	}
	x.s.LandingBatches = nil
	if stgCommitMatchesLanding(x.s, commits, 0) {
		t.Fatal("intermediate commit accepted without batch")
	}
	x.s.LandingBatches = []LandingBatch{b}
	for _, change := range []func(){
		func() { commits[0].Parents = []string{stgSHA("f")} },
		func() { commits[0].Message = last.Message },
		func() { b.R = stgSHA("f"); x.s.LandingBatches[0] = b },
	} {
		saved := append([]landing.ReleaseCommit(nil), commits...)
		change()
		if stgCommitMatchesLanding(x.s, commits, 0) {
			t.Fatal("invalid stack accepted")
		}
		commits = saved
	}
}
