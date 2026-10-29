package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

type stgPolicyFixture struct {
	h  *stgHarness
	s  StgState
	o  StgGateObservation
	f  stgGateFacts
	at time.Time
}

func stgPolicy(t *testing.T) *stgPolicyFixture {
	t.Helper()
	m := stgTestMember()
	m["R"] = stgSHA("b")
	h := stgReady(t, m)
	at := h.at
	s := h.state()
	op := m["op"].(string)
	s.Legacy.Operations = map[string]landing.Operation{op: {Task: stgTestRoot, R: stgSHA("b"), M0: stgSHA("e"), A: stgSHA("1"), Attempt: "a1", EverLanded: true}}
	r := &rules.Rule{Header: rules.Header{ID: "landing:dev", Version: 1, Effective: at.Add(-time.Hour), Expires: at.Add(time.Hour)}, Kind: rules.KindLanding,
		Landing: &rules.Landing{Tenant: "dev", Repos: []rules.LandingRepo{{Repo: "bifrost-platform", STGRelease: "bifrost-deliver-platform", Allow: []string{"console/src/components/**/*.tsx"}}}}}
	r.Landing.Limits.MaxFiles = 10
	r.Landing.Limits.MaxChangedLines = 300
	ci := stgClone(h.ci("ci-uid")["ci"].(map[string]any))
	ci["repo_url"] = landing.LandingRepos["bifrost-platform"].Remote
	o := StgGateObservation{Main: stgSHA("b"), BaseP: stgSHA("e"), ReadAt: at, CI: StgCIObservation{Status: "Succeeded", UID: "ci-uid", ReadAt: at, Accepted: ci}, Lease: StgLeaseObservation{Who: "stgrel-bifrost-platform-1-1", What: "bifrost-platform", Env: "stg", ResourceVersion: "1", Generation: 1, Expires: at.Add(5 * time.Minute)}, Workloads: stgPolicyWorkloads(),
		Commits: []landing.ReleaseCommit{{SHA: stgSHA("b"), Parents: []string{stgSHA("e")}, Message: "Change\n\nAgentrt-Task: " + stgTestRoot + "\nLanded-By: agentrt-prod/" + op + "\n", Changes: []rules.LandingChange{{OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p>Ready</p>"}}}}}
	f := stgGateFacts{Environment: "prod", Live: true, Generation: 1, Bindings: map[string]stgBinding{stgTestRoot: {Root: stgTestRoot, Tenant: "dev", Repo: "bifrost-platform", Mission: "M-1", Step: "p1", StepState: "done"}}, Rules: map[string]*rules.Rule{"dev": r}, Authority: map[string]stgLandingAuthority{op: {RuleVersion: 1}}}
	return &stgPolicyFixture{h, s, o, f, at}
}
func stgPolicyWorkloads() map[string]any {
	out := map[string]any{}
	for _, name := range []string{"platform-api", "platform-workers", "platform-console"} {
		d := stgDig("a")
		if name == "platform-console" {
			d = stgDig("c")
		}
		out[name] = map[string]any{"uid": name + "-uid", "generation": int64(1), "digest": d}
	}
	return out
}
func (x *stgPolicyFixture) decide(resend bool) StgGateDecision {
	return stgEvaluateGate(x.s, stgTestBatch, x.o, x.f, x.at, resend)
}
func stgBindingChange(x *stgPolicyFixture, fn func(*stgBinding)) {
	b := x.f.Bindings[stgTestRoot]
	fn(&b)
	x.f.Bindings[stgTestRoot] = b
}

// Each mutation begins with an allowed decision through the identical entry.
// Checking the exact reason proves negatives reach their intended layer.
func TestStgGatePolicyRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, why string
		change    func(*stgPolicyFixture)
	}{
		{"prod none", "rule", func(x *stgPolicyFixture) { x.f.Rules["dev"].Landing.Repos[0].STGRelease = "none" }},
		{"missing rule", "rule", func(x *stgPolicyFixture) { delete(x.f.Rules, "dev") }},
		{"expired at decision", "rule", func(x *stgPolicyFixture) { x.f.Rules["dev"].Expires = x.at }},
		{"tightened paths", "rule", func(x *stgPolicyFixture) { x.f.Rules["dev"].Landing.Repos[0].Deny = []string{"**"} }},
		{"live off", "disabled", func(x *stgPolicyFixture) { x.f.Live = false }},
		{"wrong environment", "disabled", func(x *stgPolicyFixture) { x.f.Environment = "stg" }},
		{"old observations", "observation_expired", func(x *stgPolicyFixture) { x.o.ReadAt = x.at.Add(-3 * time.Minute) }},
		{"main moved", "inputs_changed", func(x *stgPolicyFixture) { x.o.Main = stgSHA("f") }},
		{"backwards", "baseline_moved", func(x *stgPolicyFixture) { x.o.Commits = nil }},
		{"fork", "baseline_moved", func(x *stgPolicyFixture) { x.o.Commits[0].Parents = []string{stgSHA("9")} }},
		{"merge", "non_linear", func(x *stgPolicyFixture) { x.o.Commits[0].Parents = append(x.o.Commits[0].Parents, stgSHA("9")) }},
		{"missing baseline", "baseline_unknown", func(x *stgPolicyFixture) { x.s.Evidence = map[string]StgEvent{} }},
		{"workers absent", "baseline_moved", func(x *stgPolicyFixture) { delete(x.o.Workloads, "platform-workers") }},
		{"third party rollout", "baseline_moved", func(x *stgPolicyFixture) { x.o.Workloads["platform-api"].(map[string]any)["digest"] = stgDig("f") }},
		{"latest red", "ci_red", func(x *stgPolicyFixture) { x.o.CI.Status = "Failed" }},
		{"latest running", "ci_waiting", func(x *stgPolicyFixture) { x.o.CI.Status = "Running" }},
		{"foreign CI repo", "ci_results", func(x *stgPolicyFixture) { x.o.CI.Accepted["repo_url"] = "https://foreign/repo" }},
		{"different U", "wrong_input", func(x *stgPolicyFixture) { x.o.CI.Accepted["ui_commit"] = stgSHA("f") }},
		{"different infra", "wrong_input", func(x *stgPolicyFixture) { x.o.CI.Accepted["infra_commit"] = stgSHA("f") }},
		{"manual commit", "diff_unlanded", func(x *stgPolicyFixture) { x.o.Commits[0].Message = "Manual" }},
		{"forged op", "diff_unlanded", func(x *stgPolicyFixture) {
			x.o.Commits[0].Message = strings.ReplaceAll(x.o.Commits[0].Message, "land:one:1", "land:wrong:1")
		}},
		{"foreign task", "diff_unlanded", func(x *stgPolicyFixture) {
			x.o.Commits[0].Message = strings.ReplaceAll(x.o.Commits[0].Message, stgTestRoot, stgTestRoot2)
		}},
		{"no landing gate", "rule", func(x *stgPolicyFixture) { x.f.Authority = map[string]stgLandingAuthority{} }},
		{"no merge after landing", "no_merge_after_landing", func(x *stgPolicyFixture) { stgBindingChange(x, func(b *stgBinding) { b.NoMerge = true }) }},
		{"step dropped", "no_merge_after_landing", func(x *stgPolicyFixture) { stgBindingChange(x, func(b *stgBinding) { b.StepState = "dropped" }) }},
		{"absolute floor", "hard_floor", func(x *stgPolicyFixture) { x.o.Commits[0].Changes[0].NewPath = ".github/workflows/ci.yaml" }},
		{"third debit spent", "budget_exhausted", func(x *stgPolicyFixture) { x.s.Budget[StgBudgetKey{stgSHA("b"), 1}] = 3 }},
		{"withheld", "budget_exhausted", func(x *stgPolicyFixture) {
			x.s.Withheld[StgMemberKey{Root: stgTestRoot, Op: "land:one:1", R: stgSHA("b")}] = StgEvent{Seq: 1}
		}},
		{"inherited backoff", "backoff", func(x *stgPolicyFixture) { x.s.Debits[stgSHA("b")] = StgDebit{NotBefore: x.at.Add(time.Minute)} }},
		{"lease expired", "window", func(x *stgPolicyFixture) { x.o.Lease.Expires = x.at }},
		{"generation changed", "window", func(x *stgPolicyFixture) { x.f.Generation = 2 }},
		{"wrong who", "window", func(x *stgPolicyFixture) { x.o.Lease.Who = "another" }},
		{"wrong what", "window", func(x *stgPolicyFixture) { x.o.Lease.What = "other" }},
		{"wrong env", "window", func(x *stgPolicyFixture) { x.o.Lease.Env = "prod" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			x := stgPolicy(t)
			if d := x.decide(false); !d.Allowed {
				t.Fatalf("positive control: %+v", d)
			}
			tc.change(x)
			d := x.decide(false)
			if d.Allowed || d.Why != tc.why {
				t.Fatalf("got %+v want %s", d, tc.why)
			}
		})
	}
}

func TestStgGateWholeRangeAndSet(t *testing.T) {
	t.Parallel()
	x := stgPolicy(t)
	// An allowed member followed by an unauthorized commit must fail even
	// though the batch member itself passes the current rule.
	first := x.o.Commits[0]
	first.SHA = stgSHA("a")
	o := x.s.Legacy.Operations["land:one:1"]
	o.R = first.SHA
	x.s.Legacy.Operations["land:one:1"] = o
	x.o.Commits = []landing.ReleaseCommit{first, {SHA: stgSHA("b"), Parents: []string{first.SHA}, Message: "manual revert"}}
	if d := x.decide(false); d.Why != "diff_unlanded" {
		t.Fatal(d)
	}
	// Authorize the later commit, but leave it outside the frozen batch.
	last := &x.o.Commits[1]
	last.Message = "Later\n\nAgentrt-Task: " + stgTestRoot2 + "\nLanded-By: agentrt-prod/land:two:1\n"
	last.Changes = first.Changes
	x.s.Legacy.Operations["land:two:1"] = landing.Operation{Task: stgTestRoot2, R: last.SHA, EverLanded: true}
	x.f.Bindings[stgTestRoot2] = stgBinding{Root: stgTestRoot2, Tenant: "dev", Repo: "bifrost-platform", Mission: "M-2"}
	x.f.Authority["land:two:1"] = stgLandingAuthority{RuleVersion: 1}
	// Replace the original frozen member R with the first commit, so only
	// the later R3 is missing (no earlier budget or identity failure masks it).
	b := x.s.Batches[stgTestBatch]
	old := StgMemberKey{Root: stgTestRoot, Op: "land:one:1", R: stgSHA("b")}
	m := old
	m.R = first.SHA
	b.Members = map[StgMemberKey]int64{m: 1}
	b.Tries[1].Snapshot.Record["set"] = stgMemberValues([]StgMemberKey{m})
	x.s.Members[m] = x.s.Members[old]
	delete(x.s.Members, old)
	d := x.decide(false)
	if d.Why != "set_changed" || !d.Handoff {
		t.Fatal(d)
	}
	m2 := StgMemberKey{Root: stgTestRoot2, Op: "land:two:1", R: last.SHA}
	b.Members[m2] = 1
	x.s.Members[m2] = StgMemberState{Batch: stgTestBatch, Gen: 1}
	b.Tries[1].Snapshot.Record["set"] = stgMemberValues([]StgMemberKey{m, m2})
	if d = x.decide(false); !d.Allowed || len(d.Set) != 2 {
		t.Fatal("complete set control", d)
	}
	x.s.Budget[StgBudgetKey{m.R, 1}] = 3
	if d = x.decide(false); d.Why != "budget_exhausted" {
		t.Fatal(d)
	}
	// A trusted external cover removes only the already delivered member
	// from S; it cannot remove the remaining member's charge.
	x.s.Covers[StgCoverKey{Evidence: "cluster/cicd/proof", Member: m}] = StgEvent{Seq: 22}
	b.Tries[1].Snapshot.Record["set"] = stgMemberValues([]StgMemberKey{m2})
	if d = x.decide(false); !d.Allowed || len(d.Set) != 1 || d.Set[0] != m2 {
		t.Fatal(d)
	}
}

func TestStgGateGrantAndReversal(t *testing.T) {
	t.Parallel()
	x := stgPolicy(t)
	op := "land:one:1"
	o := x.s.Legacy.Operations[op]
	x.o.Commits[0].Changes[0].NewPath = "api/internal/agentrt/store/change.go"
	x.f.Authority[op] = stgLandingAuthority{UsedGrant: true, HasGrant: true, Grant: GrantEvidence{GrantID: "g1", ApprovalID: "approval-1", Purpose: "landing_main", STGRelease: true, IntentSeq: 12, Task: o.Task, Attempt: o.Attempt, A: o.A, R: o.R, M0: o.M0}}
	if d := x.decide(false); !d.Allowed {
		t.Fatal("core grant control", d)
	}
	for _, field := range []string{"stg", "R", "task", "attempt", "purpose", "missing"} {
		a := x.f.Authority[op]
		bad := a
		switch field {
		case "stg":
			bad.Grant.STGRelease = false
		case "R":
			bad.Grant.R = stgSHA("f")
		case "task":
			bad.Grant.Task = stgTestRoot2
		case "attempt":
			bad.Grant.Attempt = "other"
		case "purpose":
			bad.Grant.Purpose = "other"
		case "missing":
			bad.HasGrant = false
		}
		x.f.Authority[op] = bad
		if d := x.decide(false); d.Why != "rule" {
			t.Fatal(field, d)
		}
		x.f.Authority[op] = a
	}
	// A rule-authorized landed revert keeps both commits authorized; the
	// disposition is recorded later by B5-e after execution is terminal.
	x = stgPolicy(t)
	o = x.s.Legacy.Operations[op]
	o.Reverted = true
	x.s.Legacy.Operations[op] = o
	if d := x.decide(false); !d.Allowed {
		t.Fatal("landed reversal still authorizes original", d)
	}
}

func TestStgGateResendOwnsThirdDebitAndNoneStopsIt(t *testing.T) {
	t.Parallel()
	x := stgPolicy(t)
	x.h.add(x.h.start(1))
	s := x.h.state()
	s.Legacy = x.s.Legacy
	x.s = s
	x.at = x.h.at.Add(2 * time.Minute)
	x.o.ReadAt = x.at
	x.o.Lease.Expires = x.at.Add(time.Minute)
	x.o.CI.ReadAt = x.at
	x.o.CI.Accepted["read_at"] = x.at.Format(time.RFC3339Nano)
	x.s.Budget[StgBudgetKey{stgSHA("b"), 1}] = 3
	if d := x.decide(true); !d.Allowed {
		t.Fatal("third debit resend", d)
	}
	x.f.Rules["dev"].Landing.Repos[0].STGRelease = "none"
	if d := x.decide(true); d.Allowed || d.Why != "rule" {
		t.Fatal(d)
	}
}

func TestStgTrustedBaselineMonotonicAndAmbiguous(t *testing.T) {
	t.Parallel()
	x := stgPolicy(t)
	old := x.s.Evidence["cluster/cicd/base-uid"]
	newer := StgEvent{Seq: 99, Record: StgRecord(stgTestEvidence(x.at, "cluster/cicd/new", "external", "new-run"))}
	newer.Record["P"] = stgSHA("f")
	x.s.Evidence["cluster/cicd/new"] = newer
	d := x.decide(false)
	if d.Allowed || d.Why != "baseline_moved" || !d.Handoff || d.Evidence != "cluster/cicd/new" {
		t.Fatal(d)
	}
	// A late arrival of an older observation cannot roll the baseline back.
	old.Seq = 100
	x.s.Evidence["cluster/cicd/base-uid"] = old
	base, ok := StgTrustedBaseline(x.s)
	if !ok || base.String("P") != stgSHA("f") {
		t.Fatal(base, ok)
	}
	conflict := newer
	conflict.Record = StgRecord(stgClone(map[string]any(newer.Record)))
	conflict.Record["P"] = stgSHA("9")
	conflict.Record["evidence"] = "cluster/cicd/conflict"
	x.s.Evidence["cluster/cicd/conflict"] = conflict
	if _, ok = StgTrustedBaseline(x.s); ok {
		t.Fatal("ambiguous deployment became baseline")
	}
}

func TestStgGatePartialRetryKeepsAuthorizationRange(t *testing.T) {
	t.Parallel()
	x := stgPolicy(t)
	b := x.s.Batches[stgTestBatch]
	first := b.Tries[1]
	first.Started = StgEvent{Seq: 7}
	first.RunEvidence = StgEvent{Seq: 8}
	first.Verify = StgEvent{Seq: 9, Record: StgRecord{"outcome": "failed"}}
	mixed := stgPolicyWorkloads()
	mixed["platform-api"].(map[string]any)["digest"] = stgDig("f")
	first.Effect = StgEvent{Seq: 10, Record: StgRecord{"class": "this_try", "workloads": mixed}}
	b.Try = 2
	b.Tries[2] = &StgTry{Snapshot: first.Snapshot}
	x.o.Lease.Who = "stgrel-bifrost-platform-1-2"
	x.o.Workloads = mixed
	if d := x.decide(false); !d.Allowed || len(d.Set) != 1 {
		t.Fatal("partial rollout control", d)
	}
	first.RunEvidence = StgEvent{}
	if d := x.decide(false); d.Why != "baseline_moved" {
		t.Fatal("running retry", d)
	}
	first.RunEvidence = StgEvent{Seq: 8}
	first.Effect.Record["class"] = "other"
	if d := x.decide(false); d.Why != "baseline_moved" {
		t.Fatal("third-party retry", d)
	}
}

func TestStgRecoveryTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind             string
		bound, guarantee bool
		delta            time.Duration
		uid, want        string
	}{
		{"not_found", false, true, -time.Second, "", "wait"}, {"not_found", false, true, 0, "", "unknown_execution"}, {"not_found", false, false, 0, "", "unknown_execution"},
		{"not_found", true, true, 0, "", "object_lost"}, {"unknown", false, true, 0, "", "wait"}, {"unknown", true, true, 0, "", "wait"},
		{"found", false, true, -time.Second, "uid", "bound"}, {"found", true, true, 0, "uid", "bound"}, {"found", true, true, 0, "other", "object_lost"},
		{"collision", false, true, 0, "", "collision"}, {"collision", true, true, 0, "", "object_lost"},
	} {
		t.Run(fmt.Sprintf("%s/%v/%v/%s/%s", tc.kind, tc.bound, tc.guarantee, tc.delta, tc.uid), func(t *testing.T) {
			t.Parallel()
			h := stgReady(t, stgTestMember())
			h.add(h.start(1))
			tr := h.state().Batches[stgTestBatch].Tries[1]
			if tc.bound {
				tr.Bound = StgEvent{Seq: 99, Record: StgRecord{"uid": "uid"}}
			}
			at := tr.Permit.Record.Time("deadline").Add(tc.delta)
			obs := StgRunObservation{Kind: tc.kind, UID: tc.uid, IntentSeq: tr.Started.Seq, PermitSeq: tr.Permit.Seq, BoundSeq: tr.Bound.Seq, FinalizerGuaranteed: tc.guarantee, ReadAt: at}
			if got := StgRecoveryDecision(tr, obs, at); got != tc.want {
				t.Fatal(got, tc.want)
			}
			obs.Kind = "found"
			obs.UID = "uid"
			if got := StgRecoveryDecision(tr, obs, at); got != "bound" {
				t.Fatal("same-entry found control", got)
			}
		})
	}
}

func TestStgRecoveryStaleAbsenceCannotQuarantineNewBinding(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	h.add(h.start(1))
	tr := h.state().Batches[stgTestBatch].Tries[1]
	at := tr.Permit.Record.Time("deadline")
	obs := StgRunObservation{IntentSeq: tr.Started.Seq, PermitSeq: tr.Permit.Seq, Kind: "not_found", ReadAt: at, FinalizerGuaranteed: true}
	if got := StgRecoveryDecision(tr, obs, at); got != "unknown_execution" {
		t.Fatal("unbound control", got)
	}
	tr.Bound = StgEvent{Seq: 99, At: at, Record: StgRecord{"uid": "new-uid"}}
	if got := StgRecoveryDecision(tr, obs, at); got != "wait" {
		t.Fatal("stale 404 called a new binding lost", got)
	}
	obs.BoundSeq = 99
	if got := StgRecoveryDecision(tr, obs, at); got != "object_lost" {
		t.Fatal("bound absence control", got)
	}
}

func TestStgRevertedGrantRetainsOnlyConsumedProvenance(t *testing.T) {
	t.Parallel()
	x := stgPolicy(t)
	op := "land:one:1"
	o := x.s.Legacy.Operations[op]
	o.Reverted = true
	x.s.Legacy.Operations[op] = o
	g := &grantView{LandingGrant: LandingGrant{GrantID: "g1", ApprovalID: "approval-1", Op: op, Task: o.Task, Attempt: o.Attempt, R: o.R, A: o.A, M0: o.M0, Purpose: "landing_main", STGRelease: true}, ConsumedOp: op, ConsumedSeq: 11}
	e, found := stgRevertedGrant(x.s, op, []*grantView{g})
	if !found || e.IntentSeq != 11 || !e.STGRelease {
		t.Fatal("consumed control", e, found)
	}
	x.f.Authority[op] = stgLandingAuthority{UsedGrant: true, HasGrant: found, Grant: e}
	x.o.Commits[0].Changes[0].NewPath = "api/internal/agentrt/store/core.go"
	if d := x.decide(false); !d.Allowed {
		t.Fatal("reverted grant no longer authorizes its original commit", d)
	}
	for _, change := range []func(*grantView){func(g *grantView) { g.ConsumedSeq = 0 }, func(g *grantView) { g.ConsumedOp = "other" }, func(g *grantView) { g.R = stgSHA("f") }} {
		bad := *g
		change(&bad)
		if _, found := stgRevertedGrant(x.s, op, []*grantView{&bad}); found {
			t.Fatal("unconsumed or foreign grant recovered")
		}
	}
}
