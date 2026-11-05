package store

import (
	"testing"
	"time"
)

// R5:5.5-13 slice 1: the pace arithmetic, apart from the reads.
func TestPaceOf(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation(BriefZone)
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	now := at("2026-11-05 11:30")
	done := func(s string) paceMove { return paceMove{at: at(s), from: "active", to: "done"} }

	// 11-02: 4, 11-03: 5, 11-04: 1 (one more done and one reopened), today 1 (partial, not averaged).
	moves := []paceMove{
		done("2026-11-02 09:00"), done("2026-11-02 10:00"), done("2026-11-02 23:59"), done("2026-11-02 00:01"),
		done("2026-11-03 08:00"), done("2026-11-03 09:00"), done("2026-11-03 10:00"), done("2026-11-03 11:00"), done("2026-11-03 12:00"),
		done("2026-11-04 08:00"), done("2026-11-04 09:00"), {at: at("2026-11-04 20:00"), from: "done", to: "active"},
		done("2026-11-05 10:50"),
		// Before the listed week, and a move between two other states: neither counts.
		done("2026-10-29 10:00"), {at: at("2026-11-03 13:00"), from: "planned", to: "active"},
	}
	p := paceOf("R5", 99, moves, now, loc)
	if len(p.Days) != PaceDays || p.Days[0].Date != "2026-10-30" || p.Days[6].Date != "2026-11-05" || !p.Days[6].Partial || p.Days[5].Partial {
		t.Fatalf("days: %+v", p.Days)
	}
	if got := []int{p.Days[3].Done, p.Days[4].Done, p.Days[5].Done, p.Days[6].Done}; got[0] != 4 || got[1] != 5 || got[2] != 1 || got[3] != 1 {
		t.Fatalf("11-02..11-05 done = %v, want [4 5 1 1]", got)
	}
	if p.Average != 3.3 || p.ETA.Status != "ok" || p.ETA.Days != 30 || p.ETA.Date != "2026-12-05" {
		t.Fatalf("average %v eta %+v", p.Average, p.ETA)
	}
	want := "R5: 99 steps left. Last 3 full days: 3.3 steps a day (4, 5, 1). At this pace about 30 days, around 2026-12-05."
	if p.Summary != want {
		t.Fatalf("summary\n got %q\nwant %q", p.Summary, want)
	}
	if p.Timezone != "America/Chicago" || !p.ReadAt.Equal(now) {
		t.Fatalf("timezone %s read_at %v", p.Timezone, p.ReadAt)
	}

	// Nothing done in the last three full days: no date, and the sentence says why.
	idle := paceOf("R5", 99, []paceMove{done("2026-11-05 09:00")}, now, loc)
	if idle.ETA.Status != "unknown" || idle.ETA.Days != 0 || idle.ETA.Date != "" {
		t.Fatalf("idle eta %+v", idle.ETA)
	}
	if want := "R5: 99 steps left. No step was done in the last 3 full days (0, 0, 0), so there is no finish date at this pace."; idle.Summary != want {
		t.Fatalf("idle summary %q", idle.Summary)
	}
	// Reopened more than done in the window is not a negative pace.
	back := paceOf("R5", 10, []paceMove{{at: at("2026-11-04 09:00"), from: "done", to: "active"}}, now, loc)
	if back.ETA.Status != "unknown" || back.Days[5].Done != -1 {
		t.Fatalf("reopened only: %+v %+v", back.ETA, back.Days[5])
	}
	if done := paceOf("R5", 0, nil, now, loc); done.ETA.Status != "done" || done.Summary != "R5: no steps left." {
		t.Fatalf("finished round: %+v", done)
	}

	// A day is the Owner's local day: 23:30 CST on 11-04 is 05:30 UTC on 11-05.
	late := paceOf("R5", 5, []paceMove{done("2026-11-04 23:30")}, now, loc)
	if late.Days[5].Done != 1 || late.Days[6].Done != 0 {
		t.Fatalf("local day: %+v", late.Days)
	}
}

// R5:5.5-17 slice 1: the stretches of the last day no registered work covers.
func TestQuietOf(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation(BriefZone)
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	now := at("2026-11-05 11:30")
	from := now.Add(-QuietWindow)
	// 11-04 evening: a review 19:22–19:31 and a hand-in 20:00–20:03; then
	// nothing until a hand-in at 09:47 and a review overlapping it; a gap of
	// 50 minutes after it is too short to count.
	spans := [][2]time.Time{
		{at("2026-11-05 09:47"), at("2026-11-05 10:05")},
		{at("2026-11-04 19:22"), at("2026-11-04 19:31")},
		{at("2026-11-04 20:00"), at("2026-11-04 20:03")},
		{at("2026-11-05 09:59"), at("2026-11-05 10:40")},
		{at("2026-11-05 11:30"), at("2026-11-05 11:30")},
		{at("2026-11-04 08:00"), at("2026-11-04 19:00")}, // started before the window
	}
	quiet, total, sum := quietOf(spans, from, now, loc)
	if len(quiet) != 1 {
		t.Fatalf("quiet %+v", quiet)
	}
	q := quiet[0]
	if !q.From.Equal(at("2026-11-04 20:03")) || !q.To.Equal(at("2026-11-05 09:47")) || q.Minutes != 824 || total != 824 {
		t.Fatalf("quiet %+v total %d", q, total)
	}
	if want := "No registered work ran for 13.7 h of the last 24 hours (longest 11-04 20:03 – 11-05 09:47)."; sum != want {
		t.Fatalf("summary %q", sum)
	}
	// Nothing at all: the whole window is one stretch.
	quiet, total, _ = quietOf(nil, from, now, loc)
	if len(quiet) != 1 || total != 24*60 {
		t.Fatalf("empty: %+v %d", quiet, total)
	}
	// Covered throughout.
	quiet, total, sum = quietOf([][2]time.Time{{from.Add(-time.Hour), now.Add(time.Hour)}}, from, now, loc)
	if len(quiet) != 0 || total != 0 || sum != "Registered work ran in every hour of the last 24 hours." {
		t.Fatalf("covered: %+v %d %q", quiet, total, sum)
	}
}
