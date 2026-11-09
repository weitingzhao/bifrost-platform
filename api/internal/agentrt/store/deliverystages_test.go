package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

var deliveryT0 = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

func deliveryAt(min int) time.Time { return deliveryT0.Add(time.Duration(min) * time.Minute) }

func deliveryEntry(min int, kind string, kv ...any) landing.Entry {
	r := landing.Record{"kind": kind}
	for i := 0; i+1 < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return landing.Entry{At: deliveryAt(min), Record: r}
}

var (
	deliveryP0 = strings.Repeat("0", 40)
	deliveryP1 = strings.Repeat("1", 40)
	deliveryP2 = strings.Repeat("2", 40)
)

// deliveryFull is a change that went all the way, with an Owner grant.
func deliveryFull() DeliveryInput {
	return DeliveryInput{
		Attempt: DeliveryAttempt{TaskID: "t_1", AttemptID: "a_1", Executor: "worker-7", SubmittedAt: deliveryAt(0), AcceptedAt: deliveryAt(1)},
		Entries: []landing.Entry{
			deliveryEntry(2, "claimed"),
			deliveryEntry(3, "prepared"),
			deliveryEntry(4, "ci_kick", "try", 1),
			deliveryEntry(10, "ci_seen", "status", "Running"),
			deliveryEntry(20, "ci_seen", "status", "Succeeded"),
			deliveryEntry(21, "awaiting_grant"),
			deliveryEntry(50, "main_intent"),
			deliveryEntry(51, "main_result", "outcome", "ok"),
			deliveryEntry(52, "landed"),
			deliveryEntry(60, "stg_pending"),
			deliveryEntry(61, "stg_snapshot", "P", deliveryP1),
			deliveryEntry(62, "stg_started"),
			deliveryEntry(80, "stg_result", "outcome", "ok"),
			deliveryEntry(81, "stg_verified", "P", deliveryP1),
		},
		Grant:      &DeliveryGrant{Approver: "owner", ApprovedAt: deliveryAt(49)},
		Pusher:     "landing-controller",
		Verified:   []DeliveryRevision{{P: deliveryP0, At: deliveryAt(-100)}, {P: deliveryP1, At: deliveryAt(81)}},
		Cycles:     []ReleaseCycle{{Revision: deliveryP1, ProdDeployStartedAt: deliveryAt(100), ProdDeployCompletedAt: deliveryAt(110), ProdDeployResult: "succeeded", ProdGatePassedAt: deliveryAt(115), TriggeredBy: "releaser-1"}},
		DeploySeen: []DeliveryDeploySeen{},
	}
}

func deliveryStates(st []DeliveryStage) string {
	var b []string
	for _, s := range st {
		b = append(b, s.Stage+"="+s.State)
	}
	return strings.Join(b, " ")
}

func deliveryWant(t *testing.T, st []DeliveryStage, want string) {
	t.Helper()
	if len(st) != len(DeliveryStageOrder) {
		t.Fatalf("stages %d, want %d", len(st), len(DeliveryStageOrder))
	}
	for i, s := range st {
		if s.Stage != DeliveryStageOrder[i] {
			t.Fatalf("stage %d is %s, want %s", i, s.Stage, DeliveryStageOrder[i])
		}
	}
	if got := deliveryStates(st); got != want {
		t.Fatalf("states\n got %s\nwant %s", got, want)
	}
}

func TestFoldDeliveryFullRunToProd(t *testing.T) {
	t.Parallel()
	st := FoldDelivery(deliveryFull())
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=done on_main=done stg=done prod_release=done on_prod=done")
	type span struct {
		in, out           time.Time
		actor, role, from string
	}
	want := []span{
		{deliveryAt(0), deliveryAt(2), "worker-7", "executor", "attempt"},
		{deliveryAt(4), deliveryAt(20), "", "", "landing"},
		{deliveryAt(21), deliveryAt(49), "owner", "approver", "grant"},
		{deliveryAt(52), deliveryAt(60), "landing-controller", "pusher", "landing"},
		{deliveryAt(60), deliveryAt(81), "", "", "landing"},
		{deliveryAt(100), deliveryAt(110), "releaser-1", "releaser", "release_cycle"},
		{deliveryAt(115), time.Time{}, "", "", "release_cycle"},
	}
	for i, w := range want {
		s := st[i]
		if !s.EnteredAt.Equal(w.in) || !s.LeftAt.Equal(w.out) || s.Actor != w.actor || s.ActorRole != w.role || s.Source != w.from {
			t.Errorf("%s = %+v, want %+v", s.Stage, s, w)
		}
		if s.WaitsOn != "" || s.Reason != "" {
			t.Errorf("%s done but waits on %q, reason %q", s.Stage, s.WaitsOn, s.Reason)
		}
	}
	b, err := json.Marshal(st[2])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"stage", "state", "entered_at", "left_at", "actor", "actor_role", "source"} {
		if _, ok := m[k]; !ok {
			t.Errorf("JSON %s has no %s", b, k)
		}
	}
}

func TestFoldDeliveryNoGrantNeeded(t *testing.T) {
	t.Parallel()
	in := deliveryFull()
	in.Grant = nil
	var entries []landing.Entry
	for _, e := range in.Entries {
		if e.Record.String("kind") != "awaiting_grant" {
			entries = append(entries, e)
		}
	}
	in.Entries = entries
	st := FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=not_reached on_main=done stg=done prod_release=done on_prod=done")
	if st[2].Reason != "no grant needed" || !st[2].EnteredAt.IsZero() || st[2].Actor != "" {
		t.Errorf("owner_wait = %+v", st[2])
	}
	// landing_ci's end is known from ci_seen, not from the next stage.
	if !st[1].LeftAt.Equal(deliveryAt(20)) {
		t.Errorf("landing_ci left %v", st[1].LeftAt)
	}
}

func TestFoldDeliveryRedCIThenRerun(t *testing.T) {
	t.Parallel()
	in := deliveryFull()
	in.Entries = []landing.Entry{
		deliveryEntry(2, "claimed"),
		deliveryEntry(4, "ci_kick", "try", 1),
		deliveryEntry(15, "ci_seen", "status", "Failed"),
		deliveryEntry(16, "ci_rerun", "try", 2),
		deliveryEntry(30, "ci_seen", "status", "Running"),
	}
	st := FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=current owner_wait=not_reached on_main=not_reached stg=not_reached prod_release=not_reached on_prod=not_reached")
	if st[1].WaitsOn != "landing CI" || st[1].Reason != "" || !st[1].EnteredAt.Equal(deliveryAt(4)) || !st[1].LeftAt.IsZero() {
		t.Errorf("landing_ci = %+v", st[1])
	}
	if st[2].Reason != "" {
		t.Errorf("owner_wait claims %q before the change reached main", st[2].Reason)
	}

	in.Entries = append(in.Entries, deliveryEntry(40, "ci_seen", "status", "Succeeded"))
	st = FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=not_reached on_main=current stg=not_reached prod_release=not_reached on_prod=not_reached")
	if !st[1].LeftAt.Equal(deliveryAt(40)) {
		t.Errorf("landing_ci left %v, want the green run", st[1].LeftAt)
	}
	if st[3].WaitsOn != "push to main" || st[3].Actor != "" {
		t.Errorf("on_main = %+v", st[3])
	}
}

func TestFoldDeliveryParkedLanding(t *testing.T) {
	t.Parallel()
	in := deliveryFull()
	in.Entries = []landing.Entry{
		deliveryEntry(2, "claimed"),
		deliveryEntry(4, "ci_kick", "try", 1),
		deliveryEntry(40, "parked", "why", "ci_stuck"),
	}
	st := FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=current owner_wait=not_reached on_main=not_reached stg=not_reached prod_release=not_reached on_prod=not_reached")
	if st[1].Reason != "parked: ci_stuck" {
		t.Errorf("landing_ci reason %q", st[1].Reason)
	}

	// Unparked, it goes on and the reason is gone.
	in.Entries = append(in.Entries, deliveryEntry(50, "unparked"), deliveryEntry(51, "ci_kick", "try", 2))
	if st = FoldDelivery(in); st[1].State != StageCurrent || st[1].Reason != "" {
		t.Errorf("unparked landing_ci = %+v", st[1])
	}

	// A declined grant ends the wait without a grant: owner_wait stays current.
	in.Grant = nil
	in.Entries = []landing.Entry{
		deliveryEntry(2, "claimed"),
		deliveryEntry(4, "ci_kick", "try", 1),
		deliveryEntry(20, "ci_seen", "status", "Succeeded"),
		deliveryEntry(21, "awaiting_grant"),
		deliveryEntry(90, "failed", "why", "declined"),
	}
	st = FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=current on_main=not_reached stg=not_reached prod_release=not_reached on_prod=not_reached")
	if st[2].Reason != "failed: declined" || !st[2].LeftAt.IsZero() {
		t.Errorf("owner_wait = %+v", st[2])
	}
}

func TestFoldDeliveryOlderProdCycleDoesNotCount(t *testing.T) {
	t.Parallel()
	in := deliveryFull()
	in.Cycles = []ReleaseCycle{{Revision: deliveryP0, ProdDeployStartedAt: deliveryAt(90), ProdDeployCompletedAt: deliveryAt(95), ProdDeployResult: "succeeded", ProdGatePassedAt: deliveryAt(96), TriggeredBy: "releaser-0"}}
	in.DeploySeen = []DeliveryDeploySeen{{Commit: deliveryP0, At: deliveryAt(95)}}
	st := FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=done on_main=done stg=done prod_release=current on_prod=not_reached")
	if !st[5].EnteredAt.IsZero() || st[5].Actor != "" || st[5].WaitsOn != "PROD release" {
		t.Errorf("prod_release = %+v", st[5])
	}

	// A later verified P carries R too: its cycle counts.
	in.Verified = append(in.Verified, DeliveryRevision{P: deliveryP2, At: deliveryAt(120)})
	in.Cycles = append(in.Cycles, ReleaseCycle{Revision: deliveryP2, ProdDeployStartedAt: deliveryAt(130), ProdDeployCompletedAt: deliveryAt(140), ProdDeployResult: "succeeded", TriggeredBy: "releaser-2"})
	in.DeploySeen = append(in.DeploySeen, DeliveryDeploySeen{Commit: deliveryP2, At: deliveryAt(141)})
	st = FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=done on_main=done stg=done prod_release=done on_prod=done")
	if !st[5].EnteredAt.Equal(deliveryAt(130)) || !st[5].LeftAt.Equal(deliveryAt(140)) || st[5].Actor != "releaser-2" {
		t.Errorf("prod_release = %+v", st[5])
	}
	if !st[6].EnteredAt.Equal(deliveryAt(141)) || st[6].Source != "deploy_seen" {
		t.Errorf("on_prod = %+v", st[6])
	}
}

func TestFoldDeliveryCyclesMissingIsUnknown(t *testing.T) {
	t.Parallel()
	in := deliveryFull()
	in.Cycles, in.DeploySeen = nil, nil
	st := FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=done on_main=done stg=done prod_release=unknown on_prod=unknown")
	for _, s := range st[5:] {
		if s.Reason == "" || !s.EnteredAt.IsZero() || !s.LeftAt.IsZero() {
			t.Errorf("%s = %+v", s.Stage, s)
		}
	}

	// Without the verified revisions the carrying P cannot be placed either.
	in = deliveryFull()
	in.Verified = nil
	st = FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=done on_main=done stg=done prod_release=unknown on_prod=unknown")
	if st[5].Reason != "no STG-verified revisions given" {
		t.Errorf("prod_release reason %q", st[5].Reason)
	}

	// A change not yet through STG is not reached there, not unknown.
	in = deliveryFull()
	in.Cycles = nil
	in.Entries = in.Entries[:9]
	st = FoldDelivery(in)
	deliveryWant(t, st, "handed_in=done landing_ci=done owner_wait=done on_main=current stg=not_reached prod_release=not_reached on_prod=not_reached")
	if st[3].WaitsOn != "STG release" || st[3].Actor != "landing-controller" {
		t.Errorf("on_main = %+v", st[3])
	}
}

func TestDeliveryUsesExactSTGCoverageThroughPROD(t *testing.T) {
	t.Parallel()
	in := deliveryFull()
	in.Entries = in.Entries[:9] // landed, without legacy STG records
	member := StgMemberKey{Root: "task", Op: "operation", R: deliveryP0}
	state := StgState{
		Covers:   map[StgCoverKey]StgEvent{{Evidence: "run", Member: member}: {Seq: 3, At: deliveryAt(90)}},
		Evidence: map[string]StgEvent{"run": {Seq: 2, At: deliveryAt(81), Record: StgRecord{"P": deliveryP1}}},
	}
	if deliveryCoverage(state, StgMemberKey{Root: "other", Op: member.Op, R: member.R}) != nil {
		t.Fatal("another task inherited coverage")
	}
	if deliveryCoverage(state, StgMemberKey{Root: member.Root, Op: member.Op, R: deliveryP2}) != nil {
		t.Fatal("another result inherited coverage")
	}
	in.Coverage = deliveryCoverage(state, member)
	stages := FoldDelivery(in)
	for _, stage := range stages {
		if stage.State != StageDone {
			t.Fatalf("%s: %s", stage.Stage, stage.State)
		}
	}
	if stages[4].Source != "stg_coverage" {
		t.Fatal("coverage provenance lost")
	}
}

func TestFoldDeliveryCurrentProductionEvidence(t *testing.T) {
	in := deliveryFull()
	in.Cycles = nil
	in.DeploySeen = nil
	in.Production = &DeliveryProduction{Commit: deliveryP1, StartedAt: deliveryAt(100), CompletedAt: deliveryAt(110)}
	st := FoldDelivery(in)
	if st[5].State != "done" || st[6].State != "done" || st[6].Source != "release_record" || !st[6].EnteredAt.Equal(deliveryAt(110)) {
		t.Fatalf("current evidence not used: %+v", st)
	}
	for _, observed := range []DeliveryProduction{
		{Reason: "unsettled"},
		{Commit: deliveryP2, StartedAt: deliveryAt(100), CompletedAt: deliveryAt(110)},
		{Commit: deliveryP1},
	} {
		in = deliveryFull() // old success must not mask a current unknown
		in.Production = &observed
		st = FoldDelivery(in)
		if st[5].State != "unknown" || st[6].State != "unknown" || !st[6].EnteredAt.IsZero() {
			t.Fatalf("unsafe current evidence: %+v", st)
		}
	}
}
