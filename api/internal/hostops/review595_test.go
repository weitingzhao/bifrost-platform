package hostops

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReview595FreezeCannotRepairEpochIntegrity(t *testing.T) {
	tr, key, _, p := baseline(t, []Rule{testRule()})
	for _, edit := range []struct {
		name   string
		damage func(*SafetyState)
	}{
		{"missing-max", func(s *SafetyState) { s.MaxEpoch = 0 }},
		{"missing-epoch", func(s *SafetyState) { s.Epoch = 0 }},
		{"epoch-behind", func(s *SafetyState) { s.MaxEpoch++ }},
		{"max-behind", func(s *SafetyState) { s.Epoch++ }},
		{"both-missing", func(s *SafetyState) { s.Epoch, s.MaxEpoch = 0, 0 }},
		{"damaged-at-limit", func(s *SafetyState) { s.MaxEpoch = ^uint64(0) }},
	} {
		t.Run(edit.name, func(t *testing.T) {
			s := testState(p)
			edit.damage(&s)
			if s.Check("test", p.Version(), p.Head()) == nil {
				t.Fatal("negative integrity control")
			}
			for _, local := range []bool{false, true, false, true} {
				stopped, err := s.Freeze(true, local)
				if err != nil {
					t.Fatal("damaged state must still permit stop", err)
				}
				if !stopped.Frozen || (s.LocalStop || local) && !stopped.LocalStop {
					t.Fatal("stop lost")
				}
				u := Unfreeze{1, "test", stopped.Epoch, stopped.Generation, p.Version(), p.Head(), "ordinary thaw, no recovery", "2026-10-25T00:00:00Z", "2026-11-24T00:00:00Z"}
				if _, err := tr.Unfreeze(stopped, signed(t, key, u, NamespaceUnfreeze), p, testNow, testFacts().clock()); err == nil {
					t.Fatalf("Freeze repaired damaged epoch without recovery: old=%d/%d stopped=%d/%d", s.Epoch, s.MaxEpoch, stopped.Epoch, stopped.MaxEpoch)
				}
				if stopped.Epoch != s.Epoch || stopped.MaxEpoch != s.MaxEpoch || stopped.Generation != s.Generation {
					t.Fatal("stop erased integrity evidence or changed recovery generation")
				}
				// Exercise the next stop against a persisted/reloaded snapshot.
				if err := json.Unmarshal(canonical(stopped), &s); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestReview595MEffectsStayInAuthorizedScope(t *testing.T) {
	_, a := admission(t)
	r := testRule()
	r.Clauses[0].Params["service"] = []string{"exporter"}
	tr, key, _, _ := baseline(t, []Rule{r})
	a.Inventory = signed(t, key, inventory(), NamespaceInventory)
	a.InventoryMark = ArtifactMark{1, Digest([]byte(a.Inventory.Text))}
	r.InventoryDigest = a.InventoryMark.Digest
	a.PolicySet = PolicySet{signed(t, key, PolicyBundle{metadata("test-policy"), "", map[string]string{}, []Rule{r}}, NamespacePolicy), []Tightening{}}
	p, err := tr.AdmitBaseline(a.PolicySet.Baseline, nil, "test", testNow)
	if err != nil {
		t.Fatal(err)
	}
	a.State = testState(p)
	if got := tr.EvaluateM(a, testNow); !got.Allowed {
		t.Fatal("positive control", got)
	}
	for _, where := range []string{"forward", "rollback", "extra-forward", "operation"} {
		t.Run(where, func(t *testing.T) {
			b := a
			switch where {
			case "forward":
				b.ForwardEffects = []Effect{{"other", "restart", testDigest, nil, "host-a"}}
			case "rollback":
				b.RollbackEffects = []Effect{{"other", "restart", testDigest, nil, "host-a"}}
			case "extra-forward":
				b.ForwardEffects = []Effect{{"exporter", "restart", testDigest, nil, "host-a"}, {"other", "restart", testDigest, nil, "host-a"}}
			case "operation":
				b.ForwardEffects = []Effect{{"exporter", "access", testDigest, nil, "host-a"}}
			}
			if got := tr.EvaluateM(b, testNow); got.Allowed {
				t.Fatalf("M exporter restart accepts effects outside signed object/action: %+v", got)
			}
		})
	}
}

func TestReview595ReadStatusBindsEffectScope(t *testing.T) {
	tr, a := admission(t)
	x := testRequest()
	x.Action = "host_status"
	x.Params = map[string]string{"check": "service"}
	if err := tr.ValidateReadScope(a.Inventory, a.InventoryMark, x, "exporter", catalog(), []Effect{{"exporter", "read", testDigest, nil, "host-a"}}, testNow); err != nil {
		t.Fatal(err)
	}
	if err := tr.ValidateReadScope(a.Inventory, a.InventoryMark, x, "exporter", catalog(), []Effect{{"sshd", "read", testDigest, nil, "host-a"}}, testNow); err == nil {
		t.Fatal("R status reads unregistered sshd using exporter registration")
	}
}

func TestReview595CircuitObservesWhenFrozen(t *testing.T) {
	r := testRule()
	r.Clauses[0].RepeatedSuccess.MaxCount = 1
	_, _, _, p := baseline(t, []Rule{r})
	s, _ := testState(p).Freeze(true, true)
	f := testFacts()
	f.History = []Event{event("succeeded")}
	n, err := p.ObserveSuccesses(testRequest(), testNow, s, f)
	if err != nil || len(n.Circuits) != 1 || !n.Circuits[0].Open || !n.Frozen || !n.LocalStop {
		t.Fatal("frozen completion lost latch", err, n)
	}
}

func TestReview595ClockExtremeOffsets(t *testing.T) {
	for _, off := range []time.Duration{time.Duration(-1 << 63), time.Duration(1<<63 - 1)} {
		if (ClockObservation{SyncedAt: testNow, Offset: off}).Check(testNow) == nil {
			t.Fatal("duration overflow")
		}
	}
}
