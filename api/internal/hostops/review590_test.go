package hostops

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

func TestReview590UnfreezeMissingHighWater(t *testing.T) {
	tr, key, _, p := baseline(t, []Rule{testRule()})
	frozen, err := testState(p).Freeze(true, false)
	if err != nil {
		t.Fatal(err)
	}
	u := Unfreeze{1, "test", frozen.Epoch, frozen.Generation, p.Version(), p.Head(), "valid thaw", "2026-10-25T00:00:00Z", "2026-11-24T00:00:00Z"}
	sig := signed(t, key, u, NamespaceUnfreeze)
	frozen.MaxRuleVersion = 0
	next, err := tr.Unfreeze(frozen, sig, p, testNow, testFacts().clock())
	if err == nil {
		t.Fatalf("missing high water repaired by ordinary unfreeze: version=%d frozen=%v check=%v", next.MaxRuleVersion, next.Frozen, next.Check("test", p.Version(), p.Head()))
	}
}

func TestReview590SuccessCircuitRemainsLatched(t *testing.T) {
	r := testRule()
	r.Clauses[0].MaxPerDay = 100
	r.Clauses[0].MaxPerWeek = 100
	r.Clauses[0].RepeatedSuccess.MaxCount = 1
	_, _, _, p := baseline(t, []Rule{r})
	f := testFacts()
	f.History = []Event{event("succeeded")}
	if p.Allow(testRequest(), testNow, testState(p), f).Allowed {
		t.Fatal("positive circuit rejection control failed")
	}
	later := testNow.Add(2 * time.Hour)
	f.ClockSyncedAt = later
	if got := p.Allow(testRequest(), later, testState(p), f); got.Allowed {
		t.Fatalf("circuit resumed without Owner release; identical retained history: %+v", got)
	}
}

func TestReview590ReadScopeBindsRequestedService(t *testing.T) {
	tr, a := admission(t)
	x := testRequest()
	x.Action = "host_logs"
	x.Params = map[string]string{"service": "sshd", "since": "2026-10-26T11:00:00Z", "lines": "10"}
	err := tr.ValidateReadScope(a.Inventory, a.InventoryMark, x, "exporter", catalog(), []Effect{{"sshd", "read", testDigest}}, testNow)
	if err == nil {
		t.Fatal("unregistered sshd log request accepted under exporter object authorization")
	}
}

func TestReview590ReadLogLineCap(t *testing.T) {
	for _, lines := range []string{"201", "99999999", "-1", "--help", "10\n20"} {
		t.Run(lines, func(t *testing.T) {
			tr, a := admission(t)
			x := testRequest()
			x.Action = "host_logs"
			x.Params = map[string]string{"service": "exporter", "since": "2026-10-26T11:00:00Z", "lines": lines}
			err := tr.ValidateReadScope(a.Inventory, a.InventoryMark, x, "exporter", catalog(), []Effect{{"exporter", "read", testDigest}}, testNow)
			if err == nil {
				t.Fatal("invalid or excessive lines accepted")
			}
		})
	}
}

func TestReview590PolicyMustVerifyOriginalBytes(t *testing.T) {
	tr, key, s, _ := baseline(t, []Rule{testRule()})
	valid := []byte(s.Baseline.Text + "\n# \ufffd\n")
	s.Baseline.Text = string(valid)
	s.Baseline.Signature = key.Sign(t, valid, NamespacePolicy)
	head := Digest(valid)
	if _, err := tr.Load(s, head, 1); err != nil {
		t.Fatalf("valid signed control failed: %v", err)
	}
	s.Baseline.Text = string(bytes.Replace(valid, []byte("\ufffd"), []byte{0xff}, 1))
	if err := tr.verify(s.Baseline, NamespacePolicy); err == nil {
		t.Fatal("raw signature rejection control failed")
	}
	if _, err := tr.Load(s, head, 1); err == nil {
		t.Fatal("Load accepted different, invalid UTF-8 input bytes after JSON replacement normalization")
	}
}

func TestReview590UnfreezeIntegrityMatrix(t *testing.T) {
	tr, key, _, p := baseline(t, []Rule{testRule()})
	frozen, _ := testState(p).Freeze(true, true)
	u := Unfreeze{1, "test", frozen.Epoch, frozen.Generation, p.Version(), p.Head(), "valid thaw", "2026-10-25T00:00:00Z", "2026-11-24T00:00:00Z"}
	sig := signed(t, key, u, NamespaceUnfreeze)
	for _, missing := range []bool{false, true} {
		for _, epoch := range []bool{false, true} {
			for _, generation := range []bool{false, true} {
				s := frozen
				if missing {
					s.MaxRuleVersion = 0
				}
				if epoch {
					s.MaxEpoch++
				}
				if generation {
					s.Generation++
				}
				next, err := tr.Unfreeze(s, sig, p, testNow, testFacts().clock())
				if missing || epoch || generation {
					if err == nil || !reflect.DeepEqual(next, s) {
						t.Fatalf("damaged thaw changed state: missing=%v epoch=%v generation=%v", missing, epoch, generation)
					}
				} else if err != nil || next.Check("test", p.Version(), p.Head()) != nil {
					t.Fatalf("valid thaw: %v", err)
				}
			}
		}
	}
	frozen.Circuits = nil
	if _, err := tr.Unfreeze(frozen, sig, p, testNow, testFacts().clock()); err == nil {
		t.Fatal("thaw repaired missing circuit ledger")
	}
}

func TestReview590LogBoundsAndAliases(t *testing.T) {
	tr, a := admission(t)
	check := func(service, since, lines string, effects []Effect) error {
		x := testRequest()
		x.Action = "host_logs"
		x.Params = map[string]string{"service": service, "since": since, "lines": lines}
		return tr.ValidateReadScope(a.Inventory, a.InventoryMark, x, "exporter", catalog(), effects, testNow)
	}
	effects := []Effect{{"exporter", "read", testDigest}}
	since := "2026-10-26T11:00:00Z"
	for _, service := range []string{"exporter", "com.example.exporter"} {
		for _, lines := range []string{"1", "200"} {
			if err := check(service, since, lines, effects); err != nil {
				t.Fatal(service, lines, err)
			}
		}
	}
	if err := check("exporter", "2026-10-26T06:00:00-05:00", "200", []Effect{{"com.example.exporter", "read", testDigest}}); err != nil {
		t.Fatal(err)
	}
	for _, lines := range []string{"0", "020", "+1", "1.0", " 1", "1 ", "18446744073709551616", "10\r", "1\x1b[0m"} {
		if err := check("exporter", since, lines, effects); err == nil {
			t.Fatalf("bad lines %q", lines)
		}
	}
	for _, since := range []string{"--help", "yesterday", "2026-10-26T11:00:00", "2026-10-26T11:00:00.5Z", "2026-10-26T13:00:00Z", "2026-10-26T11:00:00Z\n"} {
		if err := check("exporter", since, "10", effects); err == nil {
			t.Fatalf("bad since %q", since)
		}
	}
	for _, effects := range [][]Effect{
		{{"sshd", "read", testDigest}},
		{{"exporter", "read", testDigest}, {"sshd", "read", testDigest}},
		{{"exporter", "read", Digest([]byte("different config"))}},
	} {
		if err := check("exporter", since, "10", effects); err == nil {
			t.Fatal("substituted log effect accepted")
		}
	}
	for _, service := range []string{"sshd", "org.example.sshd", "--help", "exporter\n", "exporter.service"} {
		if err := check(service, since, "10", effects); err == nil {
			t.Fatalf("unregistered service %q", service)
		}
	}
}

func TestReview590ClockRequiredAtEveryWriteGate(t *testing.T) {
	tr, key, _, policy := baseline(t, []Rule{testRule()})
	frozen, _ := testState(policy).Freeze(true, false)
	thaw := signed(t, key, Unfreeze{1, "test", frozen.Epoch, frozen.Generation, policy.Version(), policy.Head(), "thaw", "2026-10-25T00:00:00Z", "2026-11-24T00:00:00Z"}, NamespaceUnfreeze)
	plan := testPlan()
	raw := canonical(plan)
	sig := key.Sign(t, []byte(Digest(raw)), NamespaceApproval)
	state := testState(policy)
	permit := Permit{testDigest, state.Epoch, state.Generation, testNow, testNow.Add(5 * time.Second)}
	cases := []struct {
		name  string
		clock ClockObservation
		good  bool
	}{
		{"healthy", ClockObservation{SyncedAt: testNow}, true},
		{"boundary-positive", ClockObservation{SyncedAt: testNow.Add(-5 * time.Minute), Offset: 5 * time.Second}, true},
		{"boundary-negative", ClockObservation{SyncedAt: testNow, Offset: -5 * time.Second}, true},
		{"unknown", ClockObservation{}, false},
		{"stale", ClockObservation{SyncedAt: testNow.Add(-5*time.Minute - time.Nanosecond)}, false},
		{"future-sync", ClockObservation{SyncedAt: testNow.Add(time.Second)}, false},
		{"fast", ClockObservation{SyncedAt: testNow, Offset: 5*time.Second + time.Nanosecond}, false},
		{"slow", ClockObservation{SyncedAt: testNow, Offset: -5*time.Second - time.Nanosecond}, false},
		{"rollback", ClockObservation{SyncedAt: testNow, RolledBack: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := planContext(plan)
			ctx.Clock = tc.clock
			if _, err := tr.VerifyApproval(raw, sig, ctx, testNow); (err == nil) != tc.good {
				t.Fatalf("H: %v", err)
			}
			if _, err := tr.Unfreeze(frozen, thaw, policy, testNow, tc.clock); (err == nil) != tc.good {
				t.Fatalf("unfreeze: %v", err)
			}
			if err := permit.Check(state, testDigest, testNow, tc.clock); (err == nil) != tc.good {
				t.Fatalf("permit: %v", err)
			}
			f := testFacts()
			f.ClockSyncedAt = tc.clock.SyncedAt
			f.ClockOffset = tc.clock.Offset
			f.ClockRolledBack = tc.clock.RolledBack
			if got := policy.Allow(testRequest(), testNow, state, f); got.Allowed != tc.good {
				t.Fatalf("M: %+v", got)
			}
		})
	}
}
