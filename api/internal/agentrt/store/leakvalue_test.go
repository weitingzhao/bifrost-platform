package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

const (
	leakValueTA = "t_01M6RR9EJAPMN5K0TJGQN7B2QA"
	leakValueTB = "t_01M6RR9EJAPMN5K0TJGQN7B2QB"
	leakValueTC = "t_01M6RR9EJAPMN5K0TJGQN7B2QC"
)

// leakValueNow is a Friday of 2026-W45; the full weeks before it end on
// Sunday 2026-11-01 (W44).
var leakValueNow = time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)

// leakValueIn is a time in the ISO week n weeks before leakValueNow's.
func leakValueIn(n int) time.Time { return leakValueNow.AddDate(0, 0, -7*n) }

func leakValueSTG(seq int64, at time.Time, kind, batch string, fields map[string]any) leakValueSTGRecord {
	r := StgRecord{"v": "1", "kind": kind, "batch": batch}
	for k, v := range fields {
		r[k] = v
	}
	return leakValueSTGRecord{seq: seq, at: at, rec: r}
}

func leakValueMembers(roots ...string) []any {
	out := make([]any, len(roots))
	for i, r := range roots {
		out[i] = map[string]any{"root": r, "op": "land:" + r + ":1", "R": strings.Repeat("a", 40)}
	}
	return out
}

func leakValueTasks() map[string]leakValueTask {
	return map[string]leakValueTask{
		leakValueTA: {repo: "bifrost-platform", row: "A:1", rowLabel: "api", rowTitle: "Agent runtime API"},
		leakValueTB: {offPlan: true, repo: "bifrost-ui"},
		leakValueTC: {repo: "bifrost-platform"}, // on the plan, under no module
	}
}

func leakValueRowOf(t *testing.T, doc LeakValueDoc, place string) LeakValueRow {
	t.Helper()
	for _, r := range doc.Rows {
		if r.Place == place {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", place, doc.Rows)
	return LeakValueRow{}
}

func leakValueCellOf(t *testing.T, r LeakValueRow, week string) LeakValueCell {
	t.Helper()
	for _, c := range r.Cells {
		if c.Week == week {
			return c
		}
	}
	t.Fatalf("no week %s in %+v", week, r.Cells)
	return LeakValueCell{}
}

func TestLeakValueWeeks(t *testing.T) {
	t.Parallel()
	doc := leakValueFold(leakValueRows{}, leakValueNow, 8)
	if doc.Weeks != 8 || len(doc.Rows) != 0 || doc.AsOf != "2026-11-06T15:00:00Z" {
		t.Fatalf("%+v", doc)
	}
	if got := LeakValueWeek(leakValueNow); got != "2026-W45" {
		t.Fatal(got)
	}
	// ISO weeks in UTC: Sunday late is still the week before; the first days
	// of January may be the year before's.
	if got := LeakValueWeek(time.Date(2026, 11, 1, 23, 59, 0, 0, time.UTC)); got != "2026-W44" {
		t.Fatal(got)
	}
	if got := LeakValueWeek(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); got != "2026-W53" {
		t.Fatal(got)
	}
	if got := leakValueWeekStart(leakValueNow); !got.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatal(got)
	}
}

func TestLeakValueFoldBothLeaksByPlace(t *testing.T) {
	t.Parallel()
	rows := leakValueRows{tasks: leakValueTasks(), stg: []leakValueSTGRecord{
		leakValueSTG(1, leakValueIn(5), "opened", "stg:bifrost-platform:1", map[string]any{"members": leakValueMembers(leakValueTA, leakValueTB)}),
		leakValueSTG(2, leakValueIn(2), "snapshot", "stg:bifrost-platform:1", map[string]any{"try": int64(1), "set": leakValueMembers(leakValueTA, leakValueTB)}),
		// Failed, then quarantined, in one try: one leak per member root.
		leakValueSTG(3, leakValueIn(2), "verify", "stg:bifrost-platform:1", map[string]any{"try": int64(1), "outcome": "failed", "why": "health", "evidence": ""}),
		leakValueSTG(4, leakValueIn(2), "quarantined", "stg:bifrost-platform:1", map[string]any{"try": int64(1), "why": "object_lost"}),
		// A verified try is no leak.
		leakValueSTG(5, leakValueIn(1), "snapshot", "stg:bifrost-platform:1", map[string]any{"try": int64(2), "set": leakValueMembers(leakValueTA)}),
		leakValueSTG(6, leakValueIn(1), "verify", "stg:bifrost-platform:1", map[string]any{"try": int64(2), "outcome": "verified", "why": "ok", "evidence": "e1"}),
		// A quarantined try with no snapshot read: the batch's members.
		leakValueSTG(7, leakValueIn(1), "opened", "stg:bifrost-platform:2", map[string]any{"members": leakValueMembers(leakValueTA)}),
		leakValueSTG(8, leakValueIn(1), "quarantined", "stg:bifrost-platform:2", map[string]any{"try": int64(1), "why": "collision"}),
		// A verification the records leave unknown.
		leakValueSTG(9, leakValueIn(0), "opened", "stg:bifrost-platform:3", map[string]any{"members": leakValueMembers(leakValueTB)}),
		leakValueSTG(10, leakValueIn(0), "diag", "stg:bifrost-platform:3", map[string]any{"try": int64(1), "what": "verify_unknown"}),
	}, faults: []leakValueFault{
		{issue: "BI-1", check: 11, at: leakValueIn(3), mission: "M-7", task: leakValueTA},
		{issue: "BI-2", check: 12, at: leakValueIn(4)}, // no work: unattributed
		{issue: "BI-3", check: 13, at: leakValueIn(2), mission: "M-8", task: leakValueTC},
		{issue: "BI-4", check: 14, at: leakValueIn(1), mission: "M-9"}, // work with no task
	}}
	doc := leakValueFold(rows, leakValueNow, 8)
	if len(doc.Rows) != 3 || doc.Rows[0].Place != "A:1" || doc.Rows[2].Place != LeakValueUnattributed {
		t.Fatalf("rows %+v", doc.Rows)
	}

	a := leakValueRowOf(t, doc, "A:1")
	if a.Label != "api Agent runtime API" || len(a.Cells) != 9 || a.Cells[0].Week != "2026-W37" || a.Cells[8].Week != "2026-W45" {
		t.Fatalf("A:1 %+v", a)
	}
	for week, want := range map[string]LeakValueCell{"2026-W42": {PROD: 1}, "2026-W43": {STG: 1}, "2026-W44": {STG: 1}, "2026-W45": {}} {
		want.Week = week
		if got := leakValueCellOf(t, a, week); got != want {
			t.Fatalf("A:1 %s: %+v, want %+v", week, got, want)
		}
	}
	if a.Mark == nil || a.Mark.Kind != "add_back" || a.Mark.Since != "2026-W42" {
		t.Fatalf("A:1 mark %+v", a.Mark)
	}
	if len(a.Leaks) != 3 || a.Leaks[0].Env != "prod" || !strings.Contains(a.Leaks[0].Record, "issue BI-1") ||
		!strings.Contains(a.Leaks[1].Record, "stg:bifrost-platform:1 try 1 verify failed") || !strings.Contains(a.Leaks[1].Record, "seq 3") ||
		!strings.Contains(a.Leaks[2].Record, "stg:bifrost-platform:2 try 1 quarantined (collision)") {
		t.Fatalf("A:1 leaks %+v", a.Leaks)
	}

	ui := leakValueRowOf(t, doc, "bifrost-ui, off plan")
	if ui.Label != "bifrost-ui, off plan" || ui.Mark != nil {
		t.Fatalf("off plan %+v", ui)
	}
	if c := leakValueCellOf(t, ui, "2026-W43"); c.STG != 1 || c.PROD != 0 {
		t.Fatalf("off plan W43 %+v", c)
	}
	if c := leakValueCellOf(t, ui, "2026-W45"); c.Unknown != 1 || c.STG != 0 {
		t.Fatalf("off plan W45 %+v", c)
	}
	if len(ui.UnknownReasons) != 1 || !strings.Contains(ui.UnknownReasons[0], "stg:bifrost-platform:3 try 1") {
		t.Fatalf("off plan reasons %v", ui.UnknownReasons)
	}

	// A fault with no work, one with no task, and a task under no arch row:
	// unattributed, never marked, however much it leaks.
	un := leakValueRowOf(t, doc, LeakValueUnattributed)
	if un.Mark != nil || leakValueCellOf(t, un, "2026-W41").PROD != 1 || leakValueCellOf(t, un, "2026-W43").PROD != 1 ||
		leakValueCellOf(t, un, "2026-W44").PROD != 1 {
		t.Fatalf("unattributed %+v", un)
	}
	if len(un.UnknownReasons) != 1 || !strings.Contains(un.UnknownReasons[0], leakValueTC) {
		t.Fatalf("unattributed reasons %v", un.UnknownReasons)
	}
}

func TestLeakValueMarkThreshold(t *testing.T) {
	t.Parallel()
	tasks := leakValueTasks()
	fault := func(at ...time.Time) leakValueRows {
		r := leakValueRows{tasks: tasks}
		for i, x := range at {
			r.faults = append(r.faults, leakValueFault{issue: "BI-1", check: int64(i + 1), at: x, mission: "M-1", task: leakValueTA})
		}
		return r
	}
	for _, c := range []struct {
		name  string
		rows  leakValueRows
		weeks int
		since string // "" for no mark
	}{
		{"one leak", fault(leakValueIn(1)), 8, ""},
		{"two leaks in one week", fault(leakValueIn(2), leakValueIn(2)), 8, ""},
		{"one leak in each of two weeks", fault(leakValueIn(4), leakValueIn(1)), 8, "2026-W41"},
		{"the current week is not full", fault(leakValueIn(1), leakValueIn(0)), 8, ""},
		{"five weeks ago is outside the last four", fault(leakValueIn(5), leakValueIn(1)), 8, ""},
		{"three weeks, three leaks", fault(leakValueIn(3), leakValueIn(2), leakValueIn(1)), 8, "2026-W42"},
		{"the mark reads four weeks whatever is shown", fault(leakValueIn(3), leakValueIn(2)), 1, "2026-W42"},
	} {
		doc := leakValueFold(c.rows, leakValueNow, c.weeks)
		var mark *LeakValueMark
		for _, r := range doc.Rows {
			if r.Place == "A:1" {
				mark = r.Mark
				if len(r.Cells) != c.weeks+1 {
					t.Fatalf("%s: %d cells", c.name, len(r.Cells))
				}
			}
		}
		switch {
		case c.since == "" && mark != nil:
			t.Fatalf("%s: marked %+v", c.name, mark)
		case c.since != "" && (mark == nil || mark.Since != c.since || mark.Kind != "add_back"):
			t.Fatalf("%s: mark %+v, want since %s", c.name, mark, c.since)
		}
	}
	// STG and PROD leaks add up.
	r := fault(leakValueIn(2))
	r.stg = []leakValueSTGRecord{
		leakValueSTG(1, leakValueIn(3), "opened", "stg:bifrost-platform:1", map[string]any{"members": leakValueMembers(leakValueTA)}),
		leakValueSTG(2, leakValueIn(3), "verify", "stg:bifrost-platform:1", map[string]any{"try": int64(1), "outcome": "failed", "why": "health", "evidence": ""}),
	}
	if doc := leakValueFold(r, leakValueNow, 8); doc.Rows[0].Mark == nil || doc.Rows[0].Mark.Since != "2026-W42" {
		t.Fatalf("STG and PROD %+v", doc.Rows)
	}
}

func TestLeakValuePlaceDigest(t *testing.T) {
	t.Parallel()
	a, b := LeakValuePlaceDigest("A:1"), LeakValuePlaceDigest("bifrost-ui, off plan")
	if len(a) != 16 || a == b || a != LeakValuePlaceDigest("A:1") {
		t.Fatal(a, b)
	}
}

// The reads, against the database: a failed STG try of a root no task row
// names is an unattributed leak in the week of its verify record.
//
// Serial: FreezeEventClock (the replay sets the records' times).
func TestLeakValueReadsTheRecords(t *testing.T) {
	_, conn := stgPG(t)
	h := stgReady(t, stgTestMember())
	h.failed(1, "run_failed", "unchanged")
	stgPGReplay(t, conn, h.entries)
	doc, err := LeakValue(context.Background(), conn, leakValueNow, 2)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Weeks != 2 || len(doc.Rows) != 1 || doc.Rows[0].Place != LeakValueUnattributed || doc.Rows[0].Mark != nil {
		t.Fatalf("%+v", doc)
	}
	if c := leakValueCellOf(t, doc.Rows[0], "2026-W44"); c.STG != 1 || c.PROD != 0 {
		t.Fatalf("%+v", doc.Rows[0].Cells)
	}
	if _, err = LeakValue(context.Background(), conn, leakValueNow, LeakValueWeeksMax+1); err == nil {
		t.Fatal("13 weeks read")
	}
}
