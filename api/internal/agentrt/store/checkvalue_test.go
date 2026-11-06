package store

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// The check value fold (checkvalue.go), on rows built here: no database.

const (
	checkValueT1 = "t_01M6RR9EJ7B712709PE8SYPD01"
	checkValueT2 = "t_01M6RR9EJ7B712709PE8SYPD02"
	checkValueT3 = "t_01M6RR9EJ7B712709PE8SYPD03"
	checkValueA1 = "a_01M6RR9EJ7B712709PE8SYPD01"
	checkValueC1 = "1111111111111111111111111111111111111111"
	checkValueC2 = "2222222222222222222222222222222222222222"
	checkValueC3 = "3333333333333333333333333333333333333333"
)

// checkValueNow is a Friday of 2026-W45.
var checkValueNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// checkValueWeekAgo is the Tuesday noon of the week n weeks before
// checkValueNow's (0: its own).
func checkValueWeekAgo(n int) time.Time {
	return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -7*n)
}

func checkValueTestRows() checkValueRows {
	return checkValueRows{attempts: map[string][]checkValueAttempt{}, tasks: map[string]checkValueTask{}, stg: &StgState{}}
}

func checkValueCellOf(t *testing.T, doc CheckValueDoc, key, week string) CheckValueCell {
	t.Helper()
	for _, r := range doc.Rows {
		if r.Key != key {
			continue
		}
		for _, c := range r.Cells {
			if c.Week == week {
				return c
			}
		}
	}
	t.Fatalf("no cell %s %s", key, week)
	return CheckValueCell{}
}

func checkValueRowOf(t *testing.T, doc CheckValueDoc, key string) CheckValueRow {
	t.Helper()
	for _, r := range doc.Rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no row %s", key)
	return CheckValueRow{}
}

// checkValueWant checks a cell's numbers: nil wants unknown.
func checkValueWant(t *testing.T, c CheckValueCell, field string, want *int) {
	t.Helper()
	got := map[string]*int{"ran": c.Ran, "caught": c.Caught, "unique": c.Unique, "noise": c.Noise}[field]
	switch {
	case want == nil && got != nil:
		t.Fatalf("%s %s = %d, want unknown", c.Week, field, *got)
	case want == nil && c.Unknown[field] == "":
		t.Fatalf("%s %s unknown with no reason: %v", c.Week, field, c.Unknown)
	case want != nil && got == nil:
		t.Fatalf("%s %s unknown (%s), want %d", c.Week, field, c.Unknown[field], *want)
	case want != nil && *got != *want:
		t.Fatalf("%s %s = %d, want %d", c.Week, field, *got, *want)
	}
}

func checkValueN(n int) *int { return &n }

func TestCheckValueWeeksAreISOWeeksInUTC(t *testing.T) {
	t.Parallel()
	doc := checkValueFold(checkValueTestRows(), checkValueNow, CheckValueWeeksDefault)
	if len(doc.Weeks) != 9 || doc.Weeks[0] != "2026-W37" || doc.Weeks[8] != "2026-W45" {
		t.Fatal(doc.Weeks)
	}
	if doc.AsOf != "2026-11-06T12:00:00Z" {
		t.Fatal(doc.AsOf)
	}
	var keys []string
	for i, r := range doc.Rows {
		keys = append(keys, r.Key)
		if r.Order != i+1 || len(r.Cells) != 9 || r.Mark != nil {
			t.Fatalf("row %s: order %d, %d cells, mark %v", r.Key, r.Order, len(r.Cells), r.Mark)
		}
	}
	if strings.Join(keys, " ") != "self_check deterministic review landing_ci stg_release b5_coverage owner_cards new_round landing_retry budget_grant" {
		t.Fatal(keys)
	}
	// Across a year: 2027-01-01 is in 2026-W53; a Monday 00:00 starts its week.
	doc = checkValueFold(checkValueTestRows(), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 1)
	if strings.Join(doc.Weeks, " ") != "2026-W52 2026-W53" {
		t.Fatal(doc.Weeks)
	}
	doc = checkValueFold(checkValueTestRows(), time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), 2)
	if strings.Join(doc.Weeks, " ") != "2026-W43 2026-W44 2026-W45" {
		t.Fatal(doc.Weeks)
	}
	// An empty week is zeros, not unknown, where the records would say.
	c := checkValueCellOf(t, checkValueFold(checkValueTestRows(), checkValueNow, 8), "review", "2026-W44")
	checkValueWant(t, c, "ran", checkValueN(0))
	checkValueWant(t, c, "caught", checkValueN(0))
	if c.Minutes == nil || *c.Minutes != 0 || c.OwnerMinutes == nil || *c.OwnerMinutes != 0 {
		t.Fatal(c)
	}
}

func TestCheckValueSelfCheck(t *testing.T) {
	t.Parallel()
	rows := checkValueTestRows()
	since := checkValueWeekAgo(2)
	rows.selfCheckSince = &since
	rows.tasks[checkValueT1] = checkValueTask{State: "accepted", Repo: "bifrost-platform"}
	rows.tasks[checkValueT2] = checkValueTask{State: "accepted"}
	rows.computed = []checkValueComputed{
		{At: checkValueWeekAgo(3), Task: checkValueT1}, // before the release
		{At: since, Task: checkValueT1, SelfCheck: []byte(`{"runs":3,"red":1,"fixed":1,"last_green":true,"seconds":150}`)},
		{At: checkValueWeekAgo(1), Task: checkValueT1, SelfCheck: []byte(`{"runs":2,"red":2,"fixed":1,"last_green":false,"seconds":30}`)},
		{At: checkValueWeekAgo(1), Task: checkValueT1},
		{At: checkValueWeekAgo(0), Task: checkValueT2, SelfCheck: []byte(`{"runs":1,"red":1,"fixed":1,"last_green":true,"seconds":6}`)},
	}
	doc := checkValueFold(rows, checkValueNow, 8)
	old := checkValueCellOf(t, doc, "self_check", "2026-W42")
	checkValueWant(t, old, "ran", nil)
	if old.Unknown["ran"] != "recorded from R5:5.7" || old.Minutes != nil {
		t.Fatal(old)
	}
	c := checkValueCellOf(t, doc, "self_check", "2026-W43")
	checkValueWant(t, c, "ran", checkValueN(3))
	checkValueWant(t, c, "caught", checkValueN(1))
	// The self-check runs only commands the deterministic check and the
	// landing CI run again: nothing it fixes is unique.
	checkValueWant(t, c, "unique", checkValueN(0))
	checkValueWant(t, c, "noise", nil)
	if c.Minutes == nil || *c.Minutes != 2.5 {
		t.Fatal(c.Minutes)
	}
	c = checkValueCellOf(t, doc, "self_check", "2026-W44")
	checkValueWant(t, c, "ran", checkValueN(2))
	checkValueWant(t, c, "caught", checkValueN(1))
	// A task with no known repository: its catches' uniqueness is unknown.
	c = checkValueCellOf(t, doc, "self_check", "2026-W45")
	checkValueWant(t, c, "caught", checkValueN(1))
	checkValueWant(t, c, "unique", nil)
	// No self_check recorded yet: every week with an ending is unknown.
	rows.selfCheckSince = nil
	checkValueWant(t, checkValueCellOf(t, checkValueFold(rows, checkValueNow, 8), "self_check", "2026-W44"), "ran", nil)
	if covered, ok := checkValueSelfCheckCovered("bifrost-trade-infra"); !covered || !ok {
		t.Fatal("infra's hand-in is its verify", covered, ok)
	}
}

func checkValueEvidence(failed ...[]string) []byte {
	type cmd struct {
		Argv     []string `json:"argv"`
		ExitCode int      `json:"exit_code"`
	}
	v := struct {
		Commands []cmd `json:"commands"`
	}{Commands: []cmd{{Argv: []string{"go", "-C", "api", "build", "./..."}}}}
	for _, f := range failed {
		v.Commands = append(v.Commands, cmd{Argv: f, ExitCode: 1})
	}
	b, _ := json.Marshal(v)
	return b
}

func TestCheckValueVerificationsCaughtNoiseAndUnique(t *testing.T) {
	t.Parallel()
	rows := checkValueTestRows()
	at := checkValueWeekAgo(1)
	vstart, vend := at.Add(-10*time.Minute), at
	for _, task := range []string{checkValueT1, checkValueT2, checkValueT3} {
		rows.tasks[task] = checkValueTask{State: "verifying", Repo: "bifrost-platform"}
	}
	rows.tasks[checkValueT3] = checkValueTask{State: "rejected", Repo: "bifrost-platform"}
	// T1: its next attempt pushed another commit: caught. go vet is the
	// landing CI's, so the catch is not unique.
	rows.attempts[checkValueT1] = []checkValueAttempt{{No: 1, Commit: checkValueC1}, {No: 2, Commit: checkValueC2}}
	// T2: the same commit passed later: noise.
	rows.attempts[checkValueT2] = []checkValueAttempt{{No: 1, Commit: checkValueC3}, {No: 2, Commit: checkValueC3}}
	rows.verifications = []checkValueVerification{
		{Task: checkValueT1, Method: "deterministic", Verdict: "fail", Commit: checkValueC1, AttemptNo: 1, At: at,
			VerifierStarted: &vstart, VerifierEnded: &vend, Evidence: checkValueEvidence([]string{"go", "-C", "api", "vet", "./..."})},
		{Task: checkValueT2, Method: "deterministic", Verdict: "fail", Commit: checkValueC3, AttemptNo: 1, At: at,
			VerifierStarted: &vstart, VerifierEnded: &vend, Evidence: checkValueEvidence([]string{"npm", "--prefix", "console", "test"})},
		{Task: checkValueT2, Method: "deterministic", Verdict: "pass", Commit: checkValueC3, AttemptNo: 2, At: at.Add(time.Hour),
			VerifierStarted: &vstart, VerifierEnded: &vend},
		// T3 ended rejected: caught; a command no later check runs: unique.
		{Task: checkValueT3, Method: "deterministic", Verdict: "fail", Commit: checkValueC1, AttemptNo: 1, At: at,
			VerifierStarted: &vstart, VerifierEnded: &vend, Evidence: checkValueEvidence([]string{"sh", "-c", "./scripts/own-check.sh"})},
		// A review's catch is unique.
		{Task: checkValueT1, Method: "review", Verdict: "fail", Commit: checkValueC1, AttemptNo: 1, At: at, VerifierStarted: &vstart, VerifierEnded: &vend},
	}
	doc := checkValueFold(rows, checkValueNow, 8)
	c := checkValueCellOf(t, doc, "deterministic", "2026-W44")
	checkValueWant(t, c, "ran", checkValueN(4))
	checkValueWant(t, c, "caught", checkValueN(2))
	checkValueWant(t, c, "noise", checkValueN(1))
	checkValueWant(t, c, "unique", checkValueN(1))
	if c.Minutes == nil || *c.Minutes != 40 {
		t.Fatal(c.Minutes)
	}
	r := checkValueCellOf(t, doc, "review", "2026-W44")
	checkValueWant(t, r, "caught", checkValueN(1))
	checkValueWant(t, r, "unique", checkValueN(1))

	// Evidence that does not read: the week's uniqueness is unknown; a check
	// with no verifier attempt has no minutes.
	rows.verifications[0].Evidence = []byte("not json")
	rows.verifications[0].VerifierEnded = nil
	c = checkValueCellOf(t, checkValueFold(rows, checkValueNow, 8), "deterministic", "2026-W44")
	checkValueWant(t, c, "caught", checkValueN(2))
	checkValueWant(t, c, "unique", nil)
	if c.Minutes != nil || c.Unknown["minutes"] == "" {
		t.Fatal(c)
	}
	// A failure whose task has not moved on is neither yet.
	rows.attempts[checkValueT1] = rows.attempts[checkValueT1][:1]
	c = checkValueCellOf(t, checkValueFold(rows, checkValueNow, 8), "deterministic", "2026-W44")
	checkValueWant(t, c, "caught", checkValueN(1))
	if !checkValueLandingCIRuns("bifrost-platform", []string{"env", "AGENTRT_TEST_PG=", "go", "-C", "api", "test", "./x/"}) ||
		!checkValueLandingCIRuns("bifrost-platform", []string{"npm", "--prefix", "console", "run", "type-check"}) ||
		checkValueLandingCIRuns("bifrost-platform", []string{"npm", "--prefix", "console", "run", "e2e"}) ||
		checkValueLandingCIRuns("bifrost-trade-infra", []string{"go", "-C", "api", "test", "./..."}) {
		t.Fatal("the landing CI's commands")
	}
}

// checkValueLandingRecord is a landing record of op.
func checkValueLandingRecord(op, kind string, kv ...any) landing.Record {
	r := landing.Record{"v": 1, "kind": kind, "op": op}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}

// checkValueLandingRecordV is a landing record of op at version v.
func checkValueLandingRecordV(op, v, kind string, kv ...any) landing.Record {
	r := checkValueLandingRecord(op, kind, kv...)
	r["v"] = v
	return r
}

// checkValueLandingOp is an operation of task from its claim to its first
// CI kick, the entries a minute apart from at.
func checkValueLandingOp(task, op, r string, n int) []landing.Record {
	ref := "refs/heads/land/prod/" + task + "/" + string(rune('0'+n))
	return []landing.Record{
		checkValueLandingRecord(op, "claimed", "task", task, "attempt", checkValueA1, "A", r, "rule_version", 1),
		checkValueLandingRecord(op, "prepared", "R", r, "M0", checkValueC3, "committer_date", "2026-10-01T00:00:00Z", "dry_run", false),
		checkValueLandingRecord(op, "branch_intent", "ref", ref, "R", r),
		checkValueLandingRecord(op, "branch_pushed", "ref", ref, "R", r),
		checkValueLandingRecord(op, "ci_kick", "try", 1),
	}
}

func checkValueEntries(start time.Time, rs []landing.Record) []landing.Entry {
	var out []landing.Entry
	for i, r := range rs {
		out = append(out, landing.Entry{At: start.Add(time.Duration(i) * time.Minute), Record: r})
	}
	return out
}

func TestCheckValueLandingCIAndRetry(t *testing.T) {
	t.Parallel()
	start := checkValueWeekAgo(1)
	seen := func(op, run, status, r string, minute int) landing.Record {
		return checkValueLandingRecord(op, "ci_seen", "run", run, "status", status, "R", r,
			"created_at", start.Add(time.Duration(minute)*time.Minute).Format(time.RFC3339))
	}
	op1, op2, op3 := "land:"+checkValueT1+":1", "land:"+checkValueT2+":1", "land:"+checkValueT2+":2"
	var rs []landing.Record
	// T1: red, rerun green on the same R: noise; then it lands.
	rs = append(rs, checkValueLandingOp(checkValueT1, op1, checkValueC1, 1)...)                                       // 0..4, kicked at 4
	rs = append(rs, seen(op1, "ci-1", "Failed", checkValueC1, 4), checkValueLandingRecord(op1, "ci_rerun", "try", 2)) // 5, 6
	rs = append(rs, seen(op1, "ci-1b", "Succeeded", checkValueC1, 6))                                                 // 7
	rs = append(rs, checkValueLandingRecord(op1, "main_intent", "R", checkValueC1, "M0", checkValueC3),
		checkValueLandingRecord(op1, "main_result", "outcome", "ok"), checkValueLandingRecord(op1, "landed", "R", checkValueC1)) // 8..10
	// T2: red, the operation fails ci_red: caught; a retry, and its next
	// operation lands: saved.
	rs = append(rs, checkValueLandingOp(checkValueT2, op2, checkValueC2, 1)...) // 11..15
	rs = append(rs, seen(op2, "ci-2", "Failed", checkValueC2, 15), checkValueLandingRecord(op2, "failed", "why", "ci_red"),
		checkValueLandingRecordV(op2, "1.1", "retry", "root", checkValueT2, "after_op", op2, "reason", "rework accepted", "by", "controller")) // 16..18
	rs = append(rs, checkValueLandingOp(checkValueT2, op3, checkValueC3, 2)...) // 19..23
	rs = append(rs, seen(op3, "ci-3", "Succeeded", checkValueC3, 23), checkValueLandingRecord(op3, "main_intent", "R", checkValueC3, "M0", checkValueC3),
		checkValueLandingRecord(op3, "main_result", "outcome", "ok"), checkValueLandingRecord(op3, "landed", "R", checkValueC3))
	entries := checkValueEntries(start, rs)
	if _, err := landing.Fold(entries); err != nil {
		t.Fatal("fixture:", err)
	}
	rows := checkValueTestRows()
	rows.tasks[checkValueT2] = checkValueTask{State: "accepted", Repo: "bifrost-platform"}
	rows.landing = []checkValueLanding{{Repo: "bifrost-platform", Entries: entries}}
	doc := checkValueFold(rows, checkValueNow, 8)
	c := checkValueCellOf(t, doc, "landing_ci", "2026-W44")
	checkValueWant(t, c, "ran", checkValueN(4))
	checkValueWant(t, c, "noise", checkValueN(1))
	checkValueWant(t, c, "caught", checkValueN(1))
	checkValueWant(t, c, "unique", checkValueN(1))
	// From each kick or rerun to its result: 1 + 1 + 1 + 1 minutes.
	if c.Minutes == nil || *c.Minutes != 4 {
		t.Fatal(c.Minutes, c.Unknown)
	}
	r := checkValueCellOf(t, doc, "landing_retry", "2026-W44")
	checkValueWant(t, r, "ran", checkValueN(1))
	checkValueWant(t, r, "caught", checkValueN(1))
	checkValueWant(t, r, "unique", checkValueN(1))
	// A stream that does not fold is unknown, never empty.
	rows.landing[0].Entries = entries[1:]
	c = checkValueCellOf(t, checkValueFold(rows, checkValueNow, 8), "landing_ci", "2026-W44")
	checkValueWant(t, c, "ran", nil)
	if !strings.Contains(c.Unknown["ran"], "does not fold") {
		t.Fatal(c.Unknown)
	}
}

func TestCheckValueSTGReleaseAndCoverage(t *testing.T) {
	t.Parallel()
	at := checkValueWeekAgo(1)
	ev := func(seq int64, minute int, kv ...any) StgEvent {
		r := StgRecord{}
		for i := 0; i < len(kv); i += 2 {
			r[kv[i].(string)] = kv[i+1]
		}
		return StgEvent{Seq: seq, At: at.Add(time.Duration(minute) * time.Minute), Record: r}
	}
	const b1, b2 = "stg:bifrost-platform:1", "stg:bifrost-platform:2"
	s := StgState{
		Batches: map[string]*StgBatch{
			// P1: try 1 red, its retry verified: the red is noise.
			b1: {Opened: ev(1, 0, "batch", b1), Selected: ev(2, 0, "P", checkValueC1), Members: map[StgMemberKey]int64{{Root: checkValueT1}: 1},
				Tries: map[int64]*StgTry{
					1: {Started: ev(3, 0), Verify: ev(4, 20, "outcome", "failed")},
					2: {Started: ev(6, 60), Verify: ev(7, 70, "outcome", "verified")},
				}},
			// P2: quarantined and never passed: caught.
			b2: {Opened: ev(10, 100, "batch", b2), Selected: ev(11, 100, "P", checkValueC2), Members: map[StgMemberKey]int64{{Root: checkValueT2}: 1},
				Tries: map[int64]*StgTry{1: {Started: ev(12, 100), Quarantined: ev(13, 110)}}},
		},
		Records: map[int64]StgEvent{
			5:  ev(5, 30, "scope", "batch", "kind", "retry", "batch", b1, "try", 2),
			21: ev(21, 200, "scope", "block", "kind", "ci_observed", "P", checkValueC3),
			22: ev(22, 210, "scope", "block", "kind", "ci_observed", "P", checkValueC1),
		},
		BlockHistory: map[int64]StgBlock{
			// Cleared by an observation of another commit: caught.
			20: {Opened: ev(20, 150, "P", checkValueC2), Closed: ev(23, 220, "ref", 21)},
			// Cleared by the same P: noise.
			24: {Opened: ev(24, 150, "P", checkValueC1), Closed: ev(25, 220, "ref", 22)},
			// Still open.
			26: {Opened: ev(26, 150, "P", checkValueC3)},
		},
		Covers: map[StgCoverKey]StgEvent{{Evidence: "e1"}: ev(30, 300)},
	}
	rows := checkValueTestRows()
	rows.stg = &s
	doc := checkValueFold(rows, checkValueNow, 8)
	c := checkValueCellOf(t, doc, "stg_release", "2026-W44")
	checkValueWant(t, c, "ran", checkValueN(3))
	checkValueWant(t, c, "noise", checkValueN(1))
	checkValueWant(t, c, "caught", checkValueN(1))
	if c.Minutes != nil || !strings.Contains(c.Unknown["minutes"], "has no verify record") {
		t.Fatal("a quarantined try has no verify:", c)
	}
	b := checkValueCellOf(t, doc, "b5_coverage", "2026-W44")
	checkValueWant(t, b, "ran", checkValueN(4))
	checkValueWant(t, b, "caught", checkValueN(1))
	checkValueWant(t, b, "noise", checkValueN(1))
	if b.Minutes != nil || b.Unknown["minutes"] != "the coverage pass is logged, not recorded" {
		t.Fatal(b)
	}
	r := checkValueCellOf(t, doc, "landing_retry", "2026-W44")
	checkValueWant(t, r, "ran", checkValueN(1))
	checkValueWant(t, r, "caught", checkValueN(1))
	rows.stg, rows.stgErr = nil, "broken"
	c = checkValueCellOf(t, checkValueFold(rows, checkValueNow, 8), "stg_release", "2026-W44")
	checkValueWant(t, c, "ran", nil)
}

func TestCheckValueOwnerCards(t *testing.T) {
	t.Parallel()
	rows := checkValueTestRows()
	since := checkValueWeekAgo(2)
	rows.recommendedSince = &since
	rows.answers = []checkValueAnswer{
		{At: checkValueWeekAgo(3), Option: "a", By: "owner"}, // before recommendations were recorded
		{At: checkValueWeekAgo(1), Option: "a", Recommended: "a", HasRecommended: true, By: "owner"},
		{At: checkValueWeekAgo(1), Option: "b", Recommended: "a", HasRecommended: true, By: "owner"},
		{At: checkValueWeekAgo(1), Option: "c", By: "owner"}, // the card recommended none
		{At: checkValueWeekAgo(1), Option: "b", Recommended: "a", HasRecommended: true, By: "rule card-auto v3"},
	}
	doc := checkValueFold(rows, checkValueNow, 8)
	old := checkValueCellOf(t, doc, "owner_cards", "2026-W42")
	checkValueWant(t, old, "ran", checkValueN(1))
	checkValueWant(t, old, "caught", nil)
	checkValueWant(t, old, "unique", nil)
	c := checkValueCellOf(t, doc, "owner_cards", "2026-W44")
	checkValueWant(t, c, "ran", checkValueN(3))
	checkValueWant(t, c, "caught", checkValueN(2))
	checkValueWant(t, c, "unique", checkValueN(2))
	if c.OwnerMinutes == nil || *c.OwnerMinutes != 6 || !strings.HasPrefix(c.Unknown["owner_minutes_basis"], "estimated") || c.Minutes != nil {
		t.Fatal(c)
	}
}

func TestCheckValueRetriesUniqueInTheirRound(t *testing.T) {
	t.Parallel()
	rows := checkValueTestRows()
	at := checkValueWeekAgo(1)
	s1, e1 := at.Add(time.Minute), at.Add(11*time.Minute)
	s2 := at.Add(2 * time.Hour)
	rows.tasks[checkValueT1] = checkValueTask{State: "accepted"}
	rows.tasks[checkValueT2] = checkValueTask{State: "accepted"}
	rows.tasks[checkValueT3] = checkValueTask{State: "blocked"}
	// T1: a new round alone in it: a unique save; its attempts' minutes.
	rows.reworks = []checkValueRework{{Task: checkValueT1, At: at}, {Task: checkValueT2, At: at}, {Task: checkValueT3, At: at}}
	rows.attempts[checkValueT1] = []checkValueAttempt{{No: 1, Started: &s1, Ended: &e1}, {No: 2, Started: &s1, Ended: &e1}}
	// T2: a new round and a budget grant in the same round: saved, neither
	// unique.
	rows.grants = []checkValueGrant{{Task: checkValueT2, At: at.Add(time.Hour), Owner: true},
		// T3: the controller's grant, and the task not accepted: not saved.
		{Task: checkValueT3, At: at.Add(time.Hour)}}
	rows.attempts[checkValueT2] = []checkValueAttempt{{No: 1, Started: &s2}}
	doc := checkValueFold(rows, checkValueNow, 8)
	n := checkValueCellOf(t, doc, "new_round", "2026-W44")
	checkValueWant(t, n, "ran", checkValueN(3))
	checkValueWant(t, n, "caught", checkValueN(2))
	checkValueWant(t, n, "unique", checkValueN(1))
	checkValueWant(t, n, "noise", nil)
	if n.Minutes != nil || n.Unknown["minutes"] != "an attempt of the round has not ended" {
		t.Fatal("T2's attempt is running:", n)
	}
	g := checkValueCellOf(t, doc, "budget_grant", "2026-W44")
	checkValueWant(t, g, "ran", checkValueN(2))
	checkValueWant(t, g, "caught", checkValueN(1))
	checkValueWant(t, g, "unique", checkValueN(0))
	if g.OwnerMinutes == nil || *g.OwnerMinutes != 2 || g.Unknown["owner_minutes_basis"] == "" {
		t.Fatal(g)
	}
	// The grant in the round before T2's new round: both saves unique.
	rows.grants[0].At = at.Add(-30 * time.Minute)
	rows.attempts[checkValueT2] = nil
	doc = checkValueFold(rows, checkValueNow, 8)
	n = checkValueCellOf(t, doc, "new_round", "2026-W44")
	checkValueWant(t, n, "unique", checkValueN(2))
	if n.Minutes == nil || *n.Minutes != 20 {
		t.Fatal(n.Minutes, n.Unknown)
	}
	checkValueWant(t, checkValueCellOf(t, doc, "budget_grant", "2026-W44"), "unique", checkValueN(1))
}

func TestCheckValueMarkAfterFourIdleWeeks(t *testing.T) {
	t.Parallel()
	rows := checkValueTestRows()
	since := checkValueWeekAgo(12)
	rows.recommendedSince = &since
	// The Owner took the recommendation in each of the last five full weeks.
	for n := 1; n <= 5; n++ {
		rows.answers = append(rows.answers, checkValueAnswer{At: checkValueWeekAgo(n), Option: "a", Recommended: "a", HasRecommended: true, By: "owner"})
	}
	doc := checkValueFold(rows, checkValueNow, 8)
	m := checkValueRowOf(t, doc, "owner_cards").Mark
	if m == nil || m.Kind != "delete" || m.Since != "2026-W40" || m.Mission != "" {
		t.Fatal(m)
	}
	// Asked for one week, the mark still reads the last four.
	if m := checkValueRowOf(t, checkValueFold(rows, checkValueNow, 1), "owner_cards").Mark; m == nil || m.Since != "2026-W40" {
		t.Fatal(m)
	}
	// The mark carries the mission of the row's latest note.
	rows.notes = []checkValueNoteRow{{CheckValueNote: CheckValueNote{Key: "owner_cards", Week: "2026-W43", Mission: "M-7"}},
		{CheckValueNote: CheckValueNote{Key: "owner_cards", Week: "2026-W44", Mission: "M-9"}},
		{CheckValueNote: CheckValueNote{Key: "review", Week: "2026-W44", Mission: "M-8"}}}
	if m := checkValueRowOf(t, checkValueFold(rows, checkValueNow, 8), "owner_cards").Mark; m == nil || m.Mission != "M-9" {
		t.Fatal(m)
	}
	// A unique catch in one of the four weeks: no mark.
	rows.answers[1].Option = "b"
	if m := checkValueRowOf(t, checkValueFold(rows, checkValueNow, 8), "owner_cards").Mark; m != nil {
		t.Fatal(m)
	}
	// A week it did not run: no mark.
	rows.answers = slices.Delete(rows.answers, 1, 2)
	if m := checkValueRowOf(t, checkValueFold(rows, checkValueNow, 8), "owner_cards").Mark; m != nil {
		t.Fatal(m)
	}
	// Unknown uniqueness is not 0: no mark.
	rows.answers = nil
	for n := 1; n <= 4; n++ {
		rows.answers = append(rows.answers, checkValueAnswer{At: checkValueWeekAgo(n), Option: "a", By: "owner"})
	}
	rows.recommendedSince = nil
	if m := checkValueRowOf(t, checkValueFold(rows, checkValueNow, 8), "owner_cards").Mark; m != nil {
		t.Fatal(m)
	}
	// The current, partial week does not count.
	rows.recommendedSince = &since
	for i := range rows.answers {
		rows.answers[i].Recommended, rows.answers[i].HasRecommended = "a", true
	}
	rows.answers[0].At = checkValueWeekAgo(0)
	if m := checkValueRowOf(t, checkValueFold(rows, checkValueNow, 8), "owner_cards").Mark; m != nil {
		t.Fatal(m)
	}
}

func TestCheckValueCellJSON(t *testing.T) {
	t.Parallel()
	doc := checkValueFold(checkValueTestRows(), checkValueNow, 1)
	b, err := json.Marshal(checkValueRowOf(t, doc, "self_check"))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	cells := v["cells"].([]any)
	cell := cells[0].(map[string]any)
	if cell["noise"] != nil || cell["ran"] != float64(0) || cell["unknown"].(map[string]any)["noise"] == "" || v["mark"] != nil || v["kind"] != "check" {
		t.Fatal(string(b))
	}
}
