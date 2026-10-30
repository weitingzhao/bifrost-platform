package store

import (
	"reflect"
	"testing"
	"time"
)

func TestStgB4PriorityAndSnapshot(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, to, withheld, want       string
		owner, rule, covers, effective bool
	}{
		{"pending", "", "", "stg_pending", false, true, false, true},
		{"missing rule", "", "", "rule", false, false, false, true},
		{"legacy owner before rule", "", "", "owner", true, false, false, false},
		{"owner withheld before rule", "released", "owner", "owner", false, false, false, false},
		{"hard floor before rule", "released", "hard_floor", "stg_withheld", false, false, false, false},
		{"legacy and new owner", "released", "hard_floor", "owner", true, true, false, false},
		{"reversal", "disposed", "", "reverted", false, true, false, false},
		{"delivery awaits round", "delivered", "", "evidence", false, true, false, true},
		{"coverage awaits round", "", "", "evidence", false, true, true, true},
		{"delivery lost main", "delivered", "", "landed_unverified", false, true, false, false},
		{"coverage before old owner", "", "", "owner", true, true, true, true},
		{"coverage before missing rule", "", "", "rule", false, false, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newStgState()
			m := stgMember(StgRecord(stgTestMember()))
			s.Members[m] = StgMemberState{To: test.to, Ref: "proof"}
			if test.withheld != "" {
				s.Withheld[m] = StgEvent{Seq: 5, Record: StgRecord{"why": test.withheld}}
			}
			if test.covers {
				s.Covers[StgCoverKey{"cluster/cicd/run", m}] = StgEvent{Seq: 6}
			}
			ev := mainObservation{validity: "unknown", readAt: stgTestAt.Add(time.Minute)}
			if test.effective {
				ev.validity = "effective"
			}
			classify := func() LandingRootClass {
				return classifyStgRoot(LandingRootClass{Root: m.Root, Op: m.Op, Class: RootUnresolved}, s, m, test.owner, false, test.rule, ev, true)
			}
			c := classify()
			if c.Reason != test.want {
				t.Fatalf("class=%+v", c)
			}
			if (c.Class == RootDisposed) != (test.want == "owner" || test.want == "stg_withheld" || test.want == "reverted") {
				t.Fatal(c)
			}
			canDeliver := (test.to == "delivered" || test.covers) && test.effective
			if c.evidenceAt.IsZero() == canDeliver {
				t.Fatal("row 1 lost current main evidence", c)
			}
			key := c.key
			s.Budget[StgBudgetKey{m.R, 1}] = 2
			ev.readAt = ev.readAt.Add(time.Minute)
			if classify().key != key {
				t.Fatal("read_at or budget changed snapshot")
			}
			// Current-main observations narrow a round, not restart it.
			round := &closeRound{Snapshot: []string{key}, At: stgTestAt, Until: stgTestAt.Add(CloseRoundFor)}
			if !round.validFor([]string{classify().key}, ev.readAt) {
				t.Fatal("same root invalidated its round")
			}
		})
	}
}

func TestStgSnapshotTracksOnlyStableFacts(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	m := stgMember(StgRecord(stgTestMember()))
	key := stgSnapshotKey(h.state(), m)
	h.add(h.start(1))
	h.add(stgRecord("diag", "try", 1, "what", "lease_lost", "gen", 2))
	h.effective(stgTestMember(), stgSHA("b"), "effective")
	if stgSnapshotKey(h.state(), m) != key {
		t.Fatal("try, debit, diagnostic or main read changed key")
	}
	h.add(stgTestEvidence(h.at, "cluster/cicd/new", "external", "external"))
	if stgSnapshotKey(h.state(), m) != key {
		t.Fatal("unrelated evidence changed key")
	}
	h.add(stgRecord("covers", "evidence", "cluster/cicd/new", "root", m.Root, "op", m.Op, "R", m.R))
	coveredKey := stgSnapshotKey(h.state(), m)
	if coveredKey == key {
		t.Fatal("coverage identity missing from key")
	}
	h.effective(stgTestMember(), stgSHA("b"), "effective")
	if stgSnapshotKey(h.state(), m) != coveredKey {
		t.Fatal("repeat observation changed coverage identity")
	}
	h.terminal(1, "Failed")
	h.add(memberRecord(stgTestMember(), "covered", "cluster/cicd/new"))
	if stgSnapshotKey(h.state(), m) != coveredKey {
		t.Fatal("consuming existing coverage changed its identity")
	}
	h.block("baseline_moved")
	if stgSnapshotKey(h.state(), m) == coveredKey {
		t.Fatal("block identity missing from key")
	}
}

func TestStgCoverageHistoricalProofAndInMain(t *testing.T) {
	t.Parallel()
	for _, inMain := range []bool{false, true} {
		t.Run(map[bool]string{false: "landed", true: "in_main"}[inMain], func(t *testing.T) {
			t.Parallel()
			h := newStgHarness(t)
			m := stgMember(StgRecord(stgTestMember()))
			if inMain {
				m.Op = "land:" + m.Root + ":1"
				h.raw(map[string]any{"landing": stgLegacyRecord(m.Op, "failed", "v", "1.2", "why", "in_main"), "text": "the landing loop will not claim this task: in_main (attempt x, A " + stgSHA("f") + ", base " + stgSHA("e") + ", mission active; main history effective at commit " + m.R + ", read at " + stgSHA("b") + ")"})
				h.raw(map[string]any{"landing_disposed": map[string]any{"root": m.Root, "op": m.Op, "why": "owner_delivered"}})
			} else {
				h.landed(m.Op, m.Root, m.R)
			}
			h.add(stgTestEvidence(h.at, "cluster/cicd/release", "external", "manual"))
			s := h.state()
			if got := StgCoverageCandidates(s); len(got) != 1 || got[0] != m {
				t.Fatal("candidate lost H/R", got)
			}
			validity := "effective"
			if inMain {
				validity = "unknown"
			}
			positive := MainEvidence{Head: stgSHA("b"), HasR: true, Validity: validity, ReadAt: h.at}
			for _, change := range []func(*MainEvidence){func(e *MainEvidence) { e.HasR = false }, func(e *MainEvidence) { e.Head = stgSHA("c") }, func(e *MainEvidence) { e.Unreadable = true }, func(e *MainEvidence) { e.ReadAt = h.at.Add(time.Hour) }} {
				ev := positive
				change(&ev)
				if _, err := StgCoverageEvents(s, "cluster/cicd/release", m, ev, h.at); err == nil {
					t.Fatal("bad coverage accepted", ev)
				}
			}
			events, err := StgCoverageEvents(s, "cluster/cicd/release", m, positive, h.at)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range events {
				h.entries = append(h.entries, StgEntry{int64(len(h.entries) + 1), h.at, e})
			}
			s = h.state()
			if !s.covered(m) || len(StgCoverageCandidates(s)) != 0 {
				t.Fatal("coverage missing or duplicate candidate")
			}
			if inMain {
				c := classifyStgInMain(LandingRootClass{Root: m.Root, Op: m.Op, Class: RootUnresolved}, s, true, true)
				if c.Class != RootDelivered {
					t.Fatal("O-3 positive", c)
				}
				c = classifyStgInMain(LandingRootClass{Root: m.Root, Op: m.Op, Class: RootUnresolved}, s, false, true)
				if c.Class == RootDelivered {
					t.Fatal("O-3 omitted Owner", c)
				}
				delete(s.Covers, StgCoverKey{"cluster/cicd/release", m})
				c = classifyStgInMain(LandingRootClass{Root: m.Root, Op: m.Op, Class: RootUnresolved}, s, true, true)
				if c.Reason != "stg_pending" {
					t.Fatal("O-3 omitted release", c)
				}
			}
		})
	}
}

func TestStgRootStatus(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	h.add(h.start(1))
	m := stgMember(StgRecord(stgTestMember()))
	status := stgRootStatus(h.state(), m)
	if status.Batch != stgTestBatch || status.Try != 1 || status.Phase != "started" || status.BudgetUsed != 1 || status.BudgetGen != 1 {
		t.Fatal(status)
	}
	h.add(stgTestEvidence(h.at, "cluster/cicd/new-uid", "external", "manual"))
	h.add(stgRecord("quarantined", "try", 1, "why", "unknown_execution", "at", h.at.Format(time.RFC3339Nano)))
	block := h.block("baseline_moved")
	status = stgRootStatus(h.state(), m)
	if !status.Quarantined || status.Blocked == nil || status.Blocked.Why != "baseline_moved" || !status.Blocked.Since.Equal(block.At) {
		t.Fatal(status)
	}
	if !reflect.DeepEqual(status, stgRootStatus(h.state(), m)) {
		t.Fatal("status depends on process state")
	}
}
