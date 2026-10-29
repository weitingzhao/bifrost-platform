package landing

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

func waitRecord() Record {
	r := record("awaiting_grant", "soft", []string{"core", "rule_paths"}, "files", 1, "lines", 2, "material", "sha256:"+strings.Repeat("a", 64))
	r["v"] = "1.2"
	return r
}

func waitingState(t *testing.T) State {
	t.Helper()
	s := mustFold(t, phaseRecords("green", false))
	if err := s.Apply(Entry{At: epoch.Add(time.Hour), Record: waitRecord()}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestC4T45ExplicitVersionSetAndMinimums(t *testing.T) {
	t.Parallel()
	for _, v := range []any{1, "1.1", "1.2"} {
		rs := records()
		for _, r := range rs {
			r["v"] = v
		}
		s := mustFold(t, rs)
		if !s.Operations["land:1"].Reverted {
			t.Fatal(v)
		}
	}
	// A genuine 1.1 mode/retry/discard stream, independent of Version.
	rs := append([]Record{record("mode", "repo", "bifrost-platform")}, phaseRecords("discarded", false)...)
	for _, r := range rs {
		r["v"] = "1.1"
	}
	s := mustFold(t, rs)
	if s.LiveAt.IsZero() || s.Operations["land:1"].Phase != "discarded" {
		t.Fatal(s)
	}
	for _, v := range []any{1, "1.1", "1.2", "1.3", "1"} {
		r := waitRecord()
		r["v"] = v
		_, err := Parse(r)
		if (err == nil) != (v == "1.2") {
			t.Fatal(v, err)
		}
	}
	// The historical reader's version predicate is intentionally closed.
	legacy := func(r Record) bool { return r["v"] == 1 || r["v"] == "1.1" }
	if !legacy(rs[0]) || legacy(waitRecord()) {
		t.Fatal("legacy version fence")
	}
}

func TestC4WaitingClosedFieldsAndTransitions(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(Record){
		"empty":          func(r Record) { r["soft"] = []string{} },
		"unsorted":       func(r Record) { r["soft"] = []string{"size", "core"} },
		"duplicate":      func(r Record) { r["soft"] = []string{"core", "core"} },
		"unknown":        func(r Record) { r["soft"] = []string{"grant_insufficient"} },
		"digest":         func(r Record) { r["material"] = "sha256:short" },
		"negative files": func(r Record) { r["files"] = -1 },
		"negative lines": func(r Record) { r["lines"] = -1 },
		"extra":          func(r Record) { r["grant"] = "untrusted" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := waitRecord()
			if _, err := Parse(r); err != nil {
				t.Fatal(err)
			}
			mutate(r)
			if _, err := Parse(r); err == nil {
				t.Fatal("accepted invalid waiting record")
			}
		})
	}
	for name, mutate := range map[string]func(*Operation){
		"dry":        func(o *Operation) { o.DryRun = true },
		"phase":      func(o *Operation) { o.Phase = "prepared" },
		"red":        func(o *Operation) { o.CI = maps.Clone(o.CI); o.CI["status"] = "Failed" },
		"missing R":  func(o *Operation) { o.R = "" },
		"missing M0": func(o *Operation) { o.M0 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := mustFold(t, phaseRecords("green", false))
			e := Entry{At: epoch.Add(time.Hour), Record: waitRecord()}
			if err := s.Check(e); err != nil {
				t.Fatal(err)
			}
			o := s.Operations["land:1"]
			mutate(&o)
			s.Operations["land:1"] = o
			if s.Check(e) == nil {
				t.Fatal("unsafe wait")
			}
		})
	}
	s := waitingState(t)
	o := s.Operations["land:1"]
	if o.Active() || o.Terminal() || !o.EndsAtBound() || !s.RootPending(task1) {
		t.Fatal(o)
	}
	for kind, r := range map[string]Record{
		"parked":    record("parked", "why", "window", "retry_at", epoch.Add(2*time.Hour).Format(time.RFC3339)),
		"rerun":     record("ci_rerun", "try", 2),
		"repeat":    waitRecord(),
		"floor":     record("failed", "why", "floor"),
		"collision": v11("discarded", "why", "branch_collision"),
	} {
		if s.Check(Entry{At: epoch.Add(2 * time.Hour), Record: r}) == nil {
			t.Fatal(kind)
		}
	}
	for _, r := range []Record{record("ci_seen", "R", R, "run", "ci-new", "status", "Succeeded", "created_at", epoch.Add(time.Hour).Format(time.RFC3339)), record("main_intent", "R", R, "M0", M0), v11("discarded", "why", "main_changed"), record("failed", "why", "stale")} {
		if err := s.Check(Entry{At: epoch.Add(2 * time.Hour), Record: r}); err != nil {
			t.Fatal(r, err)
		}
	}
}

func TestC4T38T39GrantAvailabilityClocks(t *testing.T) {
	t.Parallel()
	for _, hours := range []int{10, 71} {
		s := waitingState(t)
		o := s.Operations["land:1"]
		grantAt := o.AwaitingAt.Add(time.Duration(hours) * time.Hour)
		o.GrantPeriods = []Interval{{Start: grantAt, End: grantAt.Add(24 * time.Hour)}}
		s.Operations["land:1"] = o
		at := grantAt.Add(30 * time.Minute)
		b := RootTiming(s, task1, at)
		if b.Waiting != time.Duration(hours)*time.Hour || b.Execution != 90*time.Minute || RootBound(s, task1, at, false) != "" {
			t.Fatal(hours, b)
		}
		// C-5 consumes the grant; pure protocol permits the edge. The C-4
		// store deliberately cannot issue this push authorization yet.
		if err := s.Apply(Entry{At: at, Record: record("main_intent", "R", R, "M0", M0)}); err != nil {
			t.Fatal(err)
		}
		if s.Operations["land:1"].EndsAtBound() || !s.Operations["land:1"].AtMain() {
			t.Fatal("timer can end main intent")
		}
		if d := NextDrill(s, "land:1", DrillObservation{}, at.Add(100*time.Hour)); d.Kind != "wait" {
			t.Fatal("drill timer ended main intent", d)
		}
	}
	s := waitingState(t)
	o := s.Operations["land:1"]
	// A grant after 40h is revoked one hour later. Waiting resumes at the
	// revocation, not at the next poll: 40+33=73h, E=2h.
	o.GrantPeriods = []Interval{{Start: epoch.Add(41 * time.Hour), End: epoch.Add(42 * time.Hour)}}
	s.Operations["land:1"] = o
	b := RootTiming(s, task1, epoch.Add(75*time.Hour))
	if b.Waiting != 73*time.Hour || b.Execution != 2*time.Hour || RootBound(s, task1, epoch.Add(75*time.Hour), false) != "approval_expired" {
		t.Fatal(b)
	}
	// A late/no grant cannot erase a limit already reached.
	s = waitingState(t)
	if RootBound(s, task1, epoch.Add(73*time.Hour), false) != "approval_expired" {
		t.Fatal("72h boundary")
	}
}

func TestC4T41T43T44GenerationsRestartAndRetry(t *testing.T) {
	t.Parallel()
	rs := phaseRecords("green", false)
	es := entries(rs)
	es = append(es, Entry{At: epoch.Add(time.Hour), Record: waitRecord()})
	whole, err := Fold(es)
	if err != nil {
		t.Fatal(err)
	}
	for cut := 0; cut <= len(es); cut++ {
		s, err := Fold(es[:cut])
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range es[cut:] {
			if err := s.Apply(e); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(s, whole) {
			t.Fatal("restart changed waiting state")
		}
	}
	s := whole
	if err := s.Apply(Entry{At: epoch.Add(41 * time.Hour), Record: v11("discarded", "why", "main_changed")}); err != nil {
		t.Fatal(err)
	}
	old := s.Operations["land:1"]
	for k := 2; k <= 8; k++ {
		op := "land:" + string(rune('0'+k))
		r := v11("claimed", "task", task1, "attempt", attempt1, "A", A, "rule_version", 1)
		r["op"] = op
		if err := s.Apply(Entry{At: epoch.Add(41 * time.Hour), Record: r}); err != nil {
			t.Fatal(err)
		}
		if k < 8 {
			r = v11("failed", "why", "bound")
			r["op"] = op
			if err := s.Apply(Entry{At: epoch.Add(41 * time.Hour), Record: r}); err != nil {
				t.Fatal(err)
			}
			r = v11("retry", "root", task1, "after_op", op, "reason", "owner retry", "by", "owner")
			r["op"] = op
			if err := s.Apply(Entry{At: epoch.Add(41 * time.Hour), Record: r}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if RootBound(s, task1, epoch.Add(42*time.Hour), false) != "" || RootBound(s, task1, epoch.Add(42*time.Hour), true) != "bound" {
		t.Fatal("eighth completion / ninth claim")
	}
	if !reflect.DeepEqual(s.Operations["land:1"], old) {
		t.Fatal("old generation mutated")
	}
	b := RootTiming(s, task1, epoch.Add(42*time.Hour))
	if b.Operations != 8 || b.Waiting != 0 || b.Execution != time.Hour {
		t.Fatal("retry did not reset E/W only", b)
	}
}

func TestC4PreparationEligibilityAndFinalPolicy(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	for _, pin := range []bool{false, true} {
		for _, path := range []string{"api/internal/research/x.go", "api/internal/agentrt/store/x.go", ".env.local"} {
			f := eligibleFacts()
			f.Environment, f.Mode, f.GrantEnabled = "prod", "live", true
			if pin {
				f.ExecutorPin = "external:coordinator"
			}
			if !EligibleToClaim(f, now).Allowed || !EligibleToPush(f, now).Allowed {
				t.Fatal("same entry success control")
			}
			f.Changes[0].OldPath, f.Changes[0].NewPath = path, path
			core := strings.Contains(path, "agentrt")
			allowed := path != ".env.local" && (core || pin)
			if EligibleToClaim(f, now).Allowed != allowed {
				t.Fatal(pin, path, EligibleToClaim(f, now))
			}
			push := EligibleToPush(f, now)
			if push.Allowed || allowed && push.Reason != "awaiting_grant" {
				t.Fatal(push)
			}
		}
	}
	if !slices.Contains(RetryableReasons, "approval_expired") || slices.Contains(RetryableReasons, "declined") {
		t.Fatal(RetryableReasons)
	}
	f := eligibleFacts()
	f.ExecutorPin = "external:coordinator"
	f.Changes = []rules.LandingChange{{NewPath: ".env", NewMode: "100644", Added: 1}}
	if reason, _ := PolicyChecks(f, now); reason != "floor" {
		t.Fatal(reason)
	}
}
