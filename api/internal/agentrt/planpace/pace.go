// Package planpace computes plan progress from observations; it does not read storage.
package planpace

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// A round's pace (R5:5.5-13 slice 1, the debt paydown plan D6 of
// 2026-11-05): how many steps are left, how many were done on each of the
// last days, and at that pace how many days remain. The coordinator reads
// this sentence instead of composing one, so a progress report cannot drift
// from the plan. Days use the caller's location; today is partial and not
// in the average. A step moved back out of done counts against its day, so a
// reopened step is not counted twice.

// PaceDays is how many local days the answer lists, today included.
const PaceDays = 7

// PaceWindow is how many complete days the average takes.
const PaceWindow = 3

// PaceDay is one local day's net count of steps done.
type PaceDay struct {
	Date    string `json:"date"`
	Done    int    `json:"done"`
	Partial bool   `json:"partial,omitempty"`
}

// PaceETA is the forecast at the measured pace, or Unknown with why.
type PaceETA struct {
	Status string `json:"status"`
	Days   int    `json:"days,omitempty"`
	Date   string `json:"date,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Pace is GET /agentrt/plan/pace's answer.
type Pace struct {
	Root      string    `json:"root"`
	ReadAt    time.Time `json:"read_at"`
	Timezone  string    `json:"timezone"`
	Remaining int       `json:"remaining"`
	Days      []PaceDay `json:"days"`
	Average   float64   `json:"average"`
	ETA       PaceETA   `json:"eta"`
	Summary   string    `json:"summary"`
	// Quiet is every stretch of the last QuietWindow, QuietMin or longer, in
	// which no registered work ran (R5:5.5-17 slice 1): no runtime attempt and
	// no registered external work. Work the runtime cannot see (a thread's own
	// shell) shows as quiet; that is the point.
	Quiet        []PaceQuiet `json:"quiet"`
	QuietMinutes int         `json:"quiet_minutes"`
	QuietSummary string      `json:"quiet_summary"`
}

// PaceQuiet is one quiet stretch.
type PaceQuiet struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Minutes int       `json:"minutes"`
}

// QuietWindow is how far back quiet stretches are read; QuietMin the
// shortest one counted.
const (
	QuietWindow = 24 * time.Hour
	QuietMin    = 60 * time.Minute
)

type Move struct {
	At       time.Time
	From, To string
}

func LocalMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Compute calculates pace from state changes; storage adapters supply the observations.
func Compute(root string, remaining int, moves []Move, now time.Time, loc *time.Location) Pace {
	today := LocalMidnight(now.In(loc))
	days := make([]PaceDay, PaceDays)
	index := map[string]int{}
	for i := range days {
		d := today.AddDate(0, 0, i-(PaceDays-1))
		days[i] = PaceDay{Date: d.Format("2006-01-02"), Partial: i == PaceDays-1}
		index[days[i].Date] = i
	}
	for _, m := range moves {
		i, ok := index[m.At.In(loc).Format("2006-01-02")]
		if !ok {
			continue
		}
		switch {
		case m.To == "done" && m.From != "done":
			days[i].Done++
		case m.From == "done" && m.To != "done":
			days[i].Done--
		}
	}
	window := days[PaceDays-1-PaceWindow : PaceDays-1]
	total := 0
	for _, d := range window {
		total += d.Done
	}
	avg := float64(total) / float64(PaceWindow)
	p := Pace{Root: root, ReadAt: now.UTC(), Timezone: loc.String(), Remaining: remaining, Days: days, Average: math.Round(avg*10) / 10}
	counts := fmt.Sprintf("%d, %d, %d", window[0].Done, window[1].Done, window[2].Done)
	switch {
	case remaining == 0:
		p.ETA = PaceETA{Status: "done"}
		p.Summary = fmt.Sprintf("%s: no steps left.", root)
	case total <= 0:
		p.ETA = PaceETA{Status: "unknown", Reason: fmt.Sprintf("no step was done in the last %d full days (%s)", PaceWindow, counts)}
		p.Summary = fmt.Sprintf("%s: %d steps left. No step was done in the last %d full days (%s), so there is no finish date at this pace.", root, remaining, PaceWindow, counts)
	default:
		n := int(math.Ceil(float64(remaining) / avg))
		date := today.AddDate(0, 0, n).Format("2006-01-02")
		p.ETA = PaceETA{Status: "ok", Days: n, Date: date}
		p.Summary = fmt.Sprintf("%s: %d steps left. Last %d full days: %.1f steps a day (%s). At this pace about %d days, around %s.", root, remaining, PaceWindow, p.Average, counts, n, date)
	}
	return p
}

// Quiet is the stretches of [from, to] no span covers, QuietMin or longer.
func Quiet(spans [][2]time.Time, from, to time.Time, loc *time.Location) ([]PaceQuiet, int, string) {
	sort.Slice(spans, func(i, j int) bool { return spans[i][0].Before(spans[j][0]) })
	quiet := []PaceQuiet{}
	at := from
	add := func(a, b time.Time) {
		if b.Sub(a) >= QuietMin {
			quiet = append(quiet, PaceQuiet{From: a.UTC(), To: b.UTC(), Minutes: int(b.Sub(a) / time.Minute)})
		}
	}
	for _, s := range spans {
		if s[1].Before(at) {
			continue
		}
		if s[0].After(at) {
			add(at, minTime(s[0], to))
		}
		if s[1].After(at) {
			at = s[1]
		}
		if !at.Before(to) {
			break
		}
	}
	if at.Before(to) {
		add(at, to)
	}
	total, longest := 0, -1
	for i, q := range quiet {
		total += q.Minutes
		if longest < 0 || q.Minutes > quiet[longest].Minutes {
			longest = i
		}
	}
	if total == 0 {
		return quiet, 0, fmt.Sprintf("Registered work ran in every hour of the last %d hours.", int(QuietWindow/time.Hour))
	}
	l := quiet[longest]
	return quiet, total, fmt.Sprintf("No registered work ran for %.1f h of the last %d hours (longest %s – %s).",
		float64(total)/60, int(QuietWindow/time.Hour), l.From.In(loc).Format("01-02 15:04"), l.To.In(loc).Format("01-02 15:04"))
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
