package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// A round's pace (R5:5.5-13 slice 1, the debt paydown plan D6 of
// 2026-11-05): how many steps are left, how many were done on each of the
// last days, and at that pace how many days remain. The coordinator reads
// this sentence instead of composing one, so a progress report cannot drift
// from the plan. Days are the Owner's (BriefZone); today is partial and not
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

// PlanPace reads a round's pace at now: the steps under root still planned
// or active, and its plan stream's moves into and out of done in the last
// PaceDays local days. A root there is not is pgx.ErrNoRows.
func PlanPace(ctx context.Context, db DB, root string, now time.Time) (Pace, error) {
	loc, lerr := time.LoadLocation(BriefZone)
	if lerr != nil {
		return Pace{}, lerr
	}
	var out Pace
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var kind string
		if err := tx.QueryRow(ctx, `SELECT node_kind FROM agentrt.plan_node WHERE plan_node_id = $1 AND parent_plan_node_id IS NULL`, root).Scan(&kind); err != nil {
			return err
		}
		var remaining int
		if err := tx.QueryRow(ctx, viewTree+`SELECT count(*) FROM tree JOIN agentrt.plan_node n USING(plan_node_id)
		  WHERE n.node_kind = 'step' AND n.state IN ('planned','active')`, root).Scan(&remaining); err != nil {
			return err
		}
		start := localMidnight(now.In(loc)).AddDate(0, 0, -(PaceDays - 1))
		rows, err := tx.Query(ctx, `SELECT at, body_json->>'from', body_json->>'to' FROM agentrt.event
		  WHERE event_stream_id = $1 AND event_type = 'plan_node_state_changed' AND at >= $2
		    AND (body_json->>'to' = 'done' OR body_json->>'from' = 'done')`, PlanStream(root), start)
		if err != nil {
			return err
		}
		var moves []paceMove
		for rows.Next() {
			var m paceMove
			if err = rows.Scan(&m.at, &m.from, &m.to); err != nil {
				rows.Close()
				return err
			}
			moves = append(moves, m)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		out = paceOf(root, remaining, moves, now, loc)
		spans, err := workSpans(ctx, tx, now.Add(-QuietWindow), now)
		if err != nil {
			return err
		}
		out.Quiet, out.QuietMinutes, out.QuietSummary = quietOf(spans, now.Add(-QuietWindow), now, loc)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Pace{}, fmt.Errorf("store: no plan %s: %w", root, err)
	}
	return out, err
}

type paceMove struct {
	at       time.Time
	from, to string
}

func localMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// paceOf is the arithmetic, apart from the reads.
func paceOf(root string, remaining int, moves []paceMove, now time.Time, loc *time.Location) Pace {
	today := localMidnight(now.In(loc))
	days := make([]PaceDay, PaceDays)
	index := map[string]int{}
	for i := range days {
		d := today.AddDate(0, 0, i-(PaceDays-1))
		days[i] = PaceDay{Date: d.Format("2006-01-02"), Partial: i == PaceDays-1}
		index[days[i].Date] = i
	}
	for _, m := range moves {
		i, ok := index[m.at.In(loc).Format("2006-01-02")]
		if !ok {
			continue
		}
		switch {
		case m.to == "done" && m.from != "done":
			days[i].Done++
		case m.from == "done" && m.to != "done":
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

// workSpans are the stretches registered work ran between from and to: each
// runtime attempt from its start to its end (or its last heartbeat while it
// runs), and each registered external work from its start (or registration)
// to its end (or last heartbeat), read from the same latest notes plan-view
// reads.
func workSpans(ctx context.Context, tx pgx.Tx, from, to time.Time) ([][2]time.Time, error) {
	var spans [][2]time.Time
	rows, err := tx.Query(ctx, `SELECT started_at, COALESCE(ended_at, heartbeat_at) FROM agentrt.attempt
	  WHERE started_at IS NOT NULL AND started_at <= $2 AND COALESCE(ended_at, heartbeat_at) >= $1`, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a, b time.Time
		if err = rows.Scan(&a, &b); err != nil {
			rows.Close()
			return nil, err
		}
		spans = append(spans, [2]time.Time{a, b})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT body FROM (SELECT DISTINCT ON(event_stream_id) body_json->'external_work' AS body FROM agentrt.event
	  WHERE event_type='note' AND body_json ? 'external_work' ORDER BY event_stream_id, seq DESC) x`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var n externalNote
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, err
		}
		start := parseNoteTime(n.StartedAt)
		if start.IsZero() {
			start = parseNoteTime(n.RegisteredAt)
		}
		end := parseNoteTime(n.EndedAt)
		if end.IsZero() {
			end = parseNoteTime(n.HeartbeatAt)
		}
		if !start.IsZero() && !end.IsZero() && !start.After(to) && !end.Before(from) {
			spans = append(spans, [2]time.Time{start, end})
		}
	}
	return spans, rows.Err()
}

func parseNoteTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// quietOf is the stretches of [from, to] no span covers, QuietMin or longer.
func quietOf(spans [][2]time.Time, from, to time.Time, loc *time.Location) ([]PaceQuiet, int, string) {
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
