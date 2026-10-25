package landing

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func record(kind string, kv ...any) Record {
	r := Record{"v": 1, "kind": kind, "op": "land:1"}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}
func records() []Record {
	ref := "refs/heads/land/prod/" + task1 + "/1"
	return []Record{record("claimed", "task", task1, "attempt", attempt1, "A", A, "rule_version", 1), record("prepared", "R", R, "M0", M0, "committer_date", epoch.Format(time.RFC3339), "dry_run", false), record("branch_intent", "ref", ref, "R", R), record("branch_pushed", "ref", ref, "R", R), record("ci_kick", "try", 1), record("ci_rerun", "try", 2), record("ci_seen", "run", "ci-1", "status", "Succeeded", "R", R, "created_at", epoch.Add(2*time.Minute).Format(time.RFC3339)), record("main_intent", "R", R, "M0", M0), record("main_result", "outcome", "ok"), record("landed", "R", R), record("reverted", "R", R, "by", A)}
}
func entries(rs []Record) []Entry {
	e := []Entry{}
	for i, r := range rs {
		e = append(e, Entry{At: epoch.Add(time.Duration(i) * time.Minute), Record: r})
	}
	return e
}
func mustFold(t *testing.T, rs []Record) State {
	t.Helper()
	s, e := Fold(entries(rs))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestLandingProtocolClosed(t *testing.T) {
	for _, r := range records() {
		if _, e := Parse(r); e != nil {
			t.Fatal(e)
		}
	}
	for _, mut := range []func(Record){func(r Record) { r["v"] = 2 }, func(r Record) { r["v"] = "1" }, func(r Record) { r["kind"] = "replaces" }, func(r Record) { r["extra"] = true }, func(r Record) { delete(r, "op") }, func(r Record) { r["A"] = "short" }, func(r Record) { r["task"] = "x" }} {
		r := records()[0]
		mut(r)
		if _, e := Parse(r); e == nil {
			t.Fatal("open protocol", r)
		}
	}
	if _, e := Parse("not object"); e == nil {
		t.Fatal("non-object accepted")
	}
}
func TestLandingRestartFoldAndTerminal(t *testing.T) {
	rs := records()
	whole := mustFold(t, rs)
	for cut := 0; cut <= len(rs); cut++ {
		s := mustFold(t, rs[:cut])
		for _, e := range entries(rs)[cut:] {
			if err := s.Apply(e); err != nil {
				t.Fatal(cut, err)
			}
		}
		if !reflect.DeepEqual(s, whole) {
			t.Fatal("restart changed state", cut)
		}
	}
	o := whole.Operations["land:1"]
	if !o.EverLanded || !o.Reverted || !o.Terminal() {
		t.Fatal("revert erased landing")
	}
	if e := whole.Apply(Entry{At: epoch.Add(time.Hour), Record: rs[0]}); e == nil {
		t.Fatal("terminal restarted")
	}
}
func TestLandingIllegalTransitions(t *testing.T) {
	rs := records()
	for n := 0; n <= len(rs); n++ {
		s := mustFold(t, rs[:n])
		before := s.Operations["land:1"]
		candidates := []Record{rs[0]}
		if n != 9 {
			candidates = append(candidates, record("landed", "R", R))
		}
		if n == 0 {
			candidates = candidates[1:]
		}
		for _, r := range candidates {
			if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: r}); e == nil {
				t.Fatalf("illegal %s from %s accepted", r.String("kind"), before.Phase)
			}
		}
	}
	for _, n := range []int{1, 2, 4} {
		s := mustFold(t, rs[:n])
		old := s.Operations["land:1"].Phase
		park := record("parked", "why", "window", "retry_at", epoch.Add(time.Hour).Format(time.RFC3339))
		if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: park}); e != nil {
			t.Fatal(e)
		}
		if s.Operations["land:1"].Active() {
			t.Fatal("park occupies slot")
		}
		if e := s.Apply(Entry{At: epoch.Add(2 * time.Hour), Record: record("unparked")}); e != nil {
			t.Fatal(e)
		}
		if s.Operations["land:1"].Phase != old {
			t.Fatal("wrong resume")
		}
	}
}
func TestLandingCIAndDryRunFences(t *testing.T) {
	rs := records()
	s := mustFold(t, rs[:4])
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: rs[7]}); e == nil {
		t.Fatal("main without CI")
	}
	for name, r := range map[string]Record{"wrong R": record("ci_seen", "run", "ci-1", "status", "Succeeded", "R", A, "created_at", epoch.Add(2*time.Minute).Format(time.RFC3339)), "old success": record("ci_seen", "run", "ci-1", "status", "Succeeded", "R", R, "created_at", epoch.Format(time.RFC3339))} {
		if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: r}); e == nil {
			t.Fatal(name)
		}
	}
	s = mustFold(t, rs[:7])
	bad := record("ci_seen", "run", "ci-2", "status", "Failed", "R", R, "created_at", epoch.Add(3*time.Minute).Format(time.RFC3339))
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: bad}); e != nil {
		t.Fatal(e)
	}
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: rs[7]}); e == nil {
		t.Fatal("older success overrides failure")
	}
	for _, r := range []Record{rs[4], rs[5]} {
		if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: r}); e == nil {
			t.Fatal("CI action repeated")
		}
	}
	rs = records()
	rs[1]["dry_run"] = true
	rs[2]["ref"] = strings.Replace(rs[2].String("ref"), "/prod/", "/stg/", 1)
	rs[3]["ref"] = rs[2]["ref"]
	s = mustFold(t, rs[:7])
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: rs[7]}); e == nil {
		t.Fatal("dry run pushed main")
	}
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: record("landed", "R", R)}); e == nil {
		t.Fatal("dry run landed")
	}
}
func TestLandingEnableAndQuotaStable(t *testing.T) {
	s := mustFold(t, []Record{record("enabled", "repo", "bifrost-platform", "rule_version", 1)})
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: record("enabled", "repo", "bifrost-platform", "rule_version", 2)}); e == nil {
		t.Fatal("enable moved on version")
	}
	if !s.EnabledAt.Equal(epoch) {
		t.Fatal("fence changed")
	}
	q := record("reserved", "root", task1)
	if e := s.Apply(Entry{At: epoch, Record: q}); e != nil {
		t.Fatal(e)
	}
	q["op"] = "land:2"
	if e := s.Apply(Entry{At: epoch, Record: q}); e == nil {
		t.Fatal("root counted twice")
	}
}
func TestLandingSTGRetryProtocol(t *testing.T) {
	dig := map[string]any{"platform-api": "sha256:" + strings.Repeat("a", 64), "platform-workers": "sha256:" + strings.Repeat("b", 64), "platform-console": "sha256:" + strings.Repeat("c", 64)}
	rs := []Record{record("stg_pending", "R", []string{R}), record("stg_snapshot", "P", R, "U", U, "try", 1, "not_before", epoch.Format(time.RFC3339)), record("stg_started", "run", "stg-1", "try", 1), record("stg_result", "run", "stg-1", "outcome", "failed")}
	s := mustFold(t, rs)
	retry := record("stg_snapshot", "P", R, "U", U, "try", 2, "not_before", epoch.Add(time.Hour+10*time.Minute).Format(time.RFC3339))
	changed := record("stg_snapshot", "P", A, "U", U, "try", 2, "not_before", retry["not_before"])
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: changed}); e == nil {
		t.Fatal("changed snapshot")
	}
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: retry}); e != nil {
		t.Fatal(e)
	}
	start := record("stg_started", "run", "stg-2", "try", 2)
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: start}); e == nil {
		t.Fatal("backoff lost")
	}
	for _, r := range []Record{start, record("stg_result", "run", "stg-2", "outcome", "ok"), record("stg_verified", "P", R, "U", U, "digests", dig)} {
		if e := s.Apply(Entry{At: epoch.Add(2 * time.Hour), Record: r}); e != nil {
			t.Fatal(e)
		}
	}
	if !s.Operations["land:1"].Terminal() {
		t.Fatal("not terminal")
	}
}

// Every forbidden phase/kind pair has a valid-shaped event. Removing a
// transition guard therefore changes the test result, not merely parsing.
func TestLandingTransitionMatrix(t *testing.T) {
	candidates := map[string]Record{}
	for _, r := range records() {
		candidates[r.String("kind")] = r
	}
	for _, r := range []Record{record("parked", "why", "window", "retry_at", epoch.Add(time.Hour).Format(time.RFC3339)), record("unparked"), record("failed", "why", "declined"), record("stg_pending", "R", []string{R}), record("stg_snapshot", "P", R, "U", U, "try", 1, "not_before", epoch.Format(time.RFC3339)), record("stg_started", "run", "stg-1", "try", 1), record("stg_result", "run", "stg-1", "outcome", "ok"), record("stg_verified", "P", R, "U", U, "digests", map[string]any{"platform-api": "sha256:" + strings.Repeat("a", 64), "platform-workers": "sha256:" + strings.Repeat("b", 64), "platform-console": "sha256:" + strings.Repeat("c", 64)}), record("stg_failed", "why", "ci_failed")} {
		candidates[r.String("kind")] = r
	}
	allowed := map[string]string{
		"": "claimed stg_pending stg_snapshot", "claimed": "prepared parked failed", "prepared": "branch_intent parked failed",
		"branch_intent": "branch_pushed failed", "branch_pushed": "ci_kick ci_seen ci_rerun parked failed main_intent",
		"parked": "unparked failed", "main_intent": "main_result", "main_result": "landed", "landed": "reverted",
		"reverted": "", "failed": "", "stg_pending": "stg_snapshot", "stg_snapshot": "stg_started", "stg_started": "stg_result",
		"stg_result": "stg_verified stg_failed", "stg_verified": "", "stg_failed": "",
	}
	for phase, permits := range allowed {
		for kind, r := range candidates {
			t.Run(phase+"_to_"+kind, func(t *testing.T) {
				s, _ := Fold(nil)
				if phase != "" {
					s.Operations["land:1"] = Operation{Phase: phase, Resume: "prepared", Task: task1, R: R, M0: M0, Ref: "refs/heads/land/prod/" + task1 + "/1", PreparedAt: epoch.Add(time.Minute), Outcome: "ok", P: R, U: U, Run: "stg-1", Try: 1, CI: record("ci_seen", "run", "ci-1", "status", "Succeeded", "R", R, "created_at", epoch.Add(2*time.Minute).Format(time.RFC3339))}
				}
				err := s.Apply(Entry{At: epoch.Add(3 * time.Hour), Record: r})
				permitted := strings.Contains(" "+permits+" ", " "+kind+" ")
				if !permitted && err == nil {
					t.Fatal("forbidden transition accepted")
				}
			})
		}
	}
}

func TestLandingNonFFEndsOperationButAllowsNext(t *testing.T) {
	rs := records()
	rs[8]["outcome"] = "non_ff"
	s := mustFold(t, rs[:9])
	o := s.Operations["land:1"]
	if !o.Terminal() || o.Active() || o.BlocksClaim() {
		t.Fatal("non-FF blocks next replay", o)
	}
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: rs[9]}); e == nil {
		t.Fatal("non-FF became landed")
	}
}
func TestLandingUnknownPushAndDenialRecovery(t *testing.T) {
	for _, outcome := range []string{"unknown", "denied"} {
		rs := records()
		rs[8]["outcome"] = outcome
		s := mustFold(t, rs[:9])
		why := "bound"
		if outcome == "denied" {
			why = "denied"
		}
		if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: record("failed", "why", why)}); e != nil {
			t.Fatal(e)
		}
		if !s.Operations["land:1"].BlocksClaim() {
			t.Fatal("failed task retried without a decision")
		}
	}
	rs := records()
	rs[8]["outcome"] = "unknown"
	s := mustFold(t, rs[:10])
	if !s.Operations["land:1"].EverLanded {
		t.Fatal("unknown push cannot reconcile")
	}
}

func TestLandingDryRunCannotConsumeQuota(t *testing.T) {
	rs := records()
	s := mustFold(t, rs[:4])
	o := s.Operations["land:1"]
	if !o.Reservable(task1) {
		t.Fatal("live result cannot reserve")
	}
	o.DryRun = true
	if o.Reservable(task1) {
		t.Fatal("dry run counted")
	}
}
