package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// checkValueWeeksOf are the full ISO weeks (UTC) before now's, oldest
// first, then now's.
func checkValueWeeksOf(now time.Time, full int) []checkValueWeek {
	day := now.UTC()
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	current := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	out := make([]checkValueWeek, 0, full+1)
	for i := full; i >= 0; i-- {
		start := current.AddDate(0, 0, -7*i)
		y, w := start.ISOWeek()
		out = append(out, checkValueWeek{label: fmt.Sprintf("%04d-W%02d", y, w), start: start, end: start.AddDate(0, 0, 7)})
	}
	return out
}

// checkValueAcc is a row's numbers in one week as the fold adds them up.
type checkValueAcc struct {
	ran, caught, unique, noise int
	seconds, ownerMinutes      float64
	unknown                    map[string]string
}

// unknownAs makes field unknown for reason, unless it already is.
func (a *checkValueAcc) unknownAs(reason string, fields ...string) {
	if a.unknown == nil {
		a.unknown = map[string]string{}
	}
	for _, f := range fields {
		if _, ok := a.unknown[f]; !ok {
			a.unknown[f] = reason
		}
	}
}

// checkValueGrid is every row's weeks.
type checkValueGrid struct {
	weeks []checkValueWeek
	rows  map[string][]checkValueAcc
}

// at is row's week of t, nil outside the weeks.
func (g checkValueGrid) at(row string, t time.Time) *checkValueAcc {
	for i, w := range g.weeks {
		if !t.Before(w.start) && t.Before(w.end) {
			return &g.rows[row][i]
		}
	}
	return nil
}

// all makes fields of every week of row unknown for reason.
func (g checkValueGrid) all(row, reason string, fields ...string) {
	for i := range g.rows[row] {
		g.rows[row][i].unknownAs(reason, fields...)
	}
}

// checkValueFold is the projection of rows at now over the last weeks full
// weeks and the current one. It does no I/O. It adds up every week a read
// may ask for, so a mark and its since are the same whatever weeks shows.
func checkValueFold(rows checkValueRows, now time.Time, weeks int) CheckValueDoc {
	g := checkValueGrid{weeks: checkValueWeeksOf(now, CheckValueWeeksMax), rows: map[string][]checkValueAcc{}}
	for _, d := range checkValueRowDefs {
		g.rows[d.key] = make([]checkValueAcc, len(g.weeks))
	}
	r := checkValueRetries{rounds: map[string][]time.Time{}}
	for _, w := range rows.reworks {
		r.rounds[w.Task] = append(r.rounds[w.Task], w.At)
	}
	checkValueSelfChecks(g, rows)
	checkValueVerifications(g, rows)
	checkValueLandings(g, rows, &r)
	checkValueSTG(g, rows, &r)
	checkValueAnswers(g, rows)
	checkValueNewRounds(g, rows, &r)
	for _, gr := range rows.grants {
		r.add(checkValueKindBudget, []string{gr.Task}, gr.At, rows.tasks[gr.Task].State == "accepted", gr.Owner)
	}
	r.fold(g)

	doc := CheckValueDoc{AsOf: now.UTC().Format(time.RFC3339Nano)}
	first := len(g.weeks) - weeks - 1
	for _, w := range g.weeks[first:] {
		doc.Weeks = append(doc.Weeks, w.label)
	}
	for i, d := range checkValueRowDefs {
		row := CheckValueRow{Key: d.key, Kind: d.kind, Label: d.label, Order: i + 1, Cells: []CheckValueCell{}}
		accs := g.rows[d.key]
		for j := first; j < len(accs); j++ {
			row.Cells = append(row.Cells, accs[j].cell(g.weeks[j].label))
		}
		row.Mark = checkValueMarkOf(d.key, accs, g.weeks, rows.notes)
		doc.Rows = append(doc.Rows, row)
	}
	return doc
}

// cell is a's numbers, each unknown one nil. What a catch is unknown of,
// its uniqueness and noise are too.
func (a checkValueAcc) cell(week string) CheckValueCell {
	unknown := map[string]string{}
	for k, v := range a.unknown {
		unknown[k] = v
	}
	if why, ok := unknown["caught"]; ok {
		for _, f := range []string{"unique", "noise"} {
			if _, has := unknown[f]; !has {
				unknown[f] = why
			}
		}
	}
	num := func(field string, n int) *int {
		if _, ok := unknown[field]; ok {
			return nil
		}
		return &n
	}
	minutes := func(field string, m float64) *float64 {
		if _, ok := unknown[field]; ok {
			return nil
		}
		m = math.Round(m*10) / 10
		return &m
	}
	return CheckValueCell{Week: week, Ran: num("ran", a.ran), Caught: num("caught", a.caught), Unique: num("unique", a.unique),
		Noise: num("noise", a.noise), Minutes: minutes("minutes", a.seconds/60), OwnerMinutes: minutes("owner_minutes", a.ownerMinutes), Unknown: unknown}
}

// checkValueMarkOf is row's mark: it ran in each of the last
// checkValueMarkWeeks full weeks with unique known and 0 in all of them;
// since is the first week of that run, and the mission the latest
// check_value note of the row names.
func checkValueMarkOf(key string, accs []checkValueAcc, weeks []checkValueWeek, notes []checkValueNoteRow) *CheckValueMark {
	idle := func(a checkValueAcc) bool {
		_, ranUnknown := a.unknown["ran"]
		_, uniqueUnknown := a.unknown["unique"]
		_, caughtUnknown := a.unknown["caught"]
		return !ranUnknown && !uniqueUnknown && !caughtUnknown && a.ran > 0 && a.unique == 0
	}
	last := len(accs) - 2 // the last full week
	since := last + 1
	for since > 0 && idle(accs[since-1]) {
		since--
	}
	if last-since+1 < checkValueMarkWeeks {
		return nil
	}
	m := &CheckValueMark{Kind: "delete", Since: weeks[since].label}
	for _, n := range notes {
		if n.Key == key && n.Mission != "" {
			m.Mission = n.Mission
		}
	}
	return m
}

// checkValueSelfChecks: an implementation's self-check (attempt_computed's
// self_check): ran its results, caught the red ones it fixed, its seconds.
// An attempt computed before the first self_check was recorded says
// nothing of it.
func checkValueSelfChecks(g checkValueGrid, rows checkValueRows) {
	g.all("self_check", "a self-check's red result is fixed in the same attempt; nothing records whether it was noise", "noise")
	for _, c := range rows.computed {
		a := g.at("self_check", c.At)
		if a == nil {
			continue
		}
		if rows.selfCheckSince == nil || c.At.Before(*rows.selfCheckSince) {
			a.unknownAs(checkValueRecordedFrom, "ran", "caught", "minutes")
			continue
		}
		if c.SelfCheck == nil {
			continue // the attempt ran no hand-in command
		}
		var sc struct{ Runs, Fixed, Seconds *int }
		if json.Unmarshal(c.SelfCheck, &sc) != nil || sc.Runs == nil || sc.Fixed == nil || sc.Seconds == nil {
			a.unknownAs("an attempt's self_check does not read", "ran", "caught", "minutes")
			continue
		}
		a.ran += *sc.Runs
		a.caught += *sc.Fixed
		a.seconds += float64(*sc.Seconds)
		repo := rows.tasks[c.Task].Repo
		switch covered, ok := checkValueSelfCheckCovered(repo); {
		case !ok:
			a.unknownAs("task "+c.Task+" names no repository whose hand-in commands are known", "unique")
		case !covered:
			a.unique += *sc.Fixed
		}
	}
}

// checkValueVerifications: the deterministic checks and the reviews. A
// failure is noise when the same commit later passed, caught when the
// task's next attempt pushed a different commit or the task ended rejected
// or cancelled, and neither while the task has not moved on. A
// deterministic catch is not unique when the landing CI runs every command
// that failed.
func checkValueVerifications(g checkValueGrid, rows checkValueRows) {
	for _, v := range rows.verifications {
		a := g.at(v.Method, v.At)
		if a == nil {
			continue
		}
		a.ran++
		if v.VerifierStarted != nil && v.VerifierEnded != nil {
			a.seconds += v.VerifierEnded.Sub(*v.VerifierStarted).Seconds()
		} else {
			a.unknownAs("a verification's verifier attempt has no start and end", "minutes")
		}
		if v.Verdict != "fail" {
			continue
		}
		switch checkValueFailure(v, rows) {
		case "noise":
			a.noise++
		case "caught":
			a.caught++
			if v.Method != "deterministic" {
				a.unique++
				continue
			}
			cmds, ok := checkValueFailedCommands(v.Evidence)
			if !ok {
				a.unknownAs("a failed check's evidence names no failing command that can be read", "unique")
				continue
			}
			repo := rows.tasks[v.Task].Repo
			if !slices.ContainsFunc(cmds, func(c []string) bool { return !checkValueLandingCIRuns(repo, c) }) {
				continue
			}
			a.unique++
		}
	}
}

// checkValueFailure is what a failed verification turned out to be:
// "noise", "caught", or "" while it cannot be told yet.
func checkValueFailure(v checkValueVerification, rows checkValueRows) string {
	if v.Commit != "" {
		for _, w := range rows.verifications {
			if w.Task == v.Task && w.Verdict == "pass" && w.Commit == v.Commit && w.At.After(v.At) {
				return "noise"
			}
		}
	}
	for _, a := range rows.attempts[v.Task] {
		if a.No > v.AttemptNo && a.Commit != "" {
			if a.Commit != v.Commit {
				return "caught"
			}
			break
		}
	}
	if s := rows.tasks[v.Task].State; s == "rejected" || s == "cancelled" {
		return "caught"
	}
	return ""
}

// checkValueCIRun is one landing CI run, from its kick or rerun to its
// result, and the operations it was the CI of (a batch's members share
// one).
type checkValueCIRun struct {
	start, at time.Time
	status, R string
	ops       []string
}

// checkValueLandings folds each landing stream as LandingState does, event
// by event: the landing CI's runs (landing_ci) and the landing's retries.
// A red run is noise when a rerun of its operation went green on the same
// R, caught when it is its operation's last run, and neither when a later
// red rerun carries the catch.
func checkValueLandings(g checkValueGrid, rows checkValueRows, r *checkValueRetries) {
	g.all("landing_ci", "nothing is recorded of the Owner's time on a landing CI", "owner_minutes")
	for _, l := range rows.landing {
		if l.Err != "" {
			g.all("landing_ci", "the landing stream of "+l.Repo+" does not read: "+l.Err, "ran", "caught", "minutes")
			g.all("landing_retry", "the landing stream of "+l.Repo+" does not read: "+l.Err, "ran", "caught", "unique")
			continue
		}
		s := landing.State{Operations: map[string]landing.Operation{}, Reservations: map[string]string{}, Grants: l.Grants}
		runs := map[string]*checkValueCIRun{}
		var order []string
		opRuns := map[string][]string{}
		start := map[string]time.Time{}
		type retry struct {
			root, next string
			at         time.Time
		}
		var retries []*retry
		pending := map[string]*retry{}
		failed := ""
		for _, e := range l.Entries {
			rec := e.Record
			kind, op := rec.String("kind"), rec.String("op")
			before := s.Operations[op]
			if err := s.Apply(e); err != nil {
				failed = err.Error()
				break
			}
			switch kind {
			case "ci_kick", "ci_rerun":
				start[op] = e.At
			case "ci_seen":
				status, name := rec.String("status"), rec.String("run")
				if status != "Succeeded" && status != "Failed" {
					break
				}
				if before.CI != nil && before.CI.String("run") == name && before.CI.String("status") == status {
					break
				}
				run := runs[name]
				if run == nil {
					run = &checkValueCIRun{start: start[op], at: e.At, status: status, R: rec.String("R")}
					runs[name] = run
					order = append(order, name)
				}
				if !slices.Contains(run.ops, op) {
					run.ops = append(run.ops, op)
					opRuns[op] = append(opRuns[op], name)
				}
			case "retry":
				x := &retry{root: rec.String("root"), at: e.At}
				retries = append(retries, x)
				pending[x.root] = x
			case "claimed":
				if x := pending[rec.String("task")]; x != nil {
					x.next = op
					delete(pending, x.root)
				}
			}
		}
		if failed != "" {
			g.all("landing_ci", "the landing stream of "+l.Repo+" does not fold: "+failed, "ran", "caught", "minutes")
			g.all("landing_retry", "the landing stream of "+l.Repo+" does not fold: "+failed, "ran", "caught", "unique")
			continue
		}
		for _, name := range order {
			run := runs[name]
			a := g.at("landing_ci", run.at)
			if a == nil {
				continue
			}
			a.ran++
			if run.start.IsZero() {
				a.unknownAs("a landing CI run has no kick on its stream", "minutes")
			} else {
				a.seconds += run.at.Sub(run.start).Seconds()
			}
			if run.status != "Failed" {
				continue
			}
			outcome := "caught"
			for _, op := range run.ops {
				later := opRuns[op][slices.Index(opRuns[op], name)+1:]
				for _, n := range later {
					if runs[n].status == "Succeeded" && runs[n].R == run.R {
						outcome = "noise"
					} else if outcome == "caught" {
						outcome = ""
					}
				}
			}
			switch outcome {
			case "noise":
				a.noise++
			case "caught":
				a.caught++
				a.unique++
			}
		}
		for _, x := range retries {
			r.add(checkValueKindLanding, []string{x.root}, x.at, x.next != "" && s.Operations[x.next].EverLanded, false)
		}
	}
}

// checkValueSTG reads the STG release (note.stg_release on
// landing:bifrost-platform) for its tries (stg_release), the B5 coverage
// pass's blocks and covers (b5_coverage) and its retries and resends.
func checkValueSTG(g checkValueGrid, rows checkValueRows, r *checkValueRetries) {
	g.all("b5_coverage", "the coverage pass is logged, not recorded", "minutes")
	if rows.stg == nil {
		why := "the STG release stream does not read: " + rows.stgErr
		g.all("stg_release", why, "ran", "caught", "minutes")
		g.all("b5_coverage", why, "ran", "caught")
		g.all("landing_retry", why, "ran", "caught", "unique")
		return
	}
	s := rows.stg
	// A try from started to verify: red (verify failed) or quarantined is
	// caught, unless a later try of the same P passed (noise).
	type try struct {
		p     string
		start time.Time
		pass  bool
	}
	var tries []try
	for _, b := range s.Batches {
		for _, t := range b.Tries {
			if t.Started.Seq > 0 {
				tries = append(tries, try{p: b.Selected.Record.String("P"), start: t.Started.At, pass: t.Verify.Record.String("outcome") == "verified"})
			}
		}
	}
	for _, b := range s.Batches {
		p := b.Selected.Record.String("P")
		for n, t := range b.Tries {
			if t.Started.Seq == 0 {
				continue
			}
			a := g.at("stg_release", t.Started.At)
			if a == nil {
				continue
			}
			a.ran++
			if t.Verify.Seq > 0 {
				a.seconds += t.Verify.At.Sub(t.Started.At).Seconds()
			} else {
				a.unknownAs(fmt.Sprintf("try %d of %s has no verify record", n, b.Opened.Record.String("batch")), "minutes")
			}
			if t.Verify.Record.String("outcome") != "failed" && t.Quarantined.Seq == 0 {
				continue
			}
			if slices.ContainsFunc(tries, func(o try) bool { return o.p == p && o.pass && o.start.After(t.Started.At) }) {
				a.noise++
			} else {
				a.caught++
				a.unique++
			}
		}
	}
	// A block cleared by a different ref is caught; by the same P, noise.
	for _, b := range s.BlockHistory {
		a := g.at("b5_coverage", b.Opened.At)
		if a == nil {
			continue
		}
		a.ran++
		if b.Closed.Seq == 0 {
			continue
		}
		seq, _ := cardNumber(b.Closed.Record["ref"])
		proof := s.Records[seq].Record
		p := proof.String("P")
		if p == "" {
			p = proof.String("head")
		}
		if p == "" {
			p = proof.object("ci").String("platform_commit")
		}
		if p != "" && p == b.Opened.Record.String("P") {
			a.noise++
		} else {
			a.caught++
			a.unique++
		}
	}
	for _, c := range s.Covers {
		if a := g.at("b5_coverage", c.At); a != nil {
			a.ran++
		}
	}
	// A retry or a resend of a try is saved when the try it leads to passed.
	seqs := make([]int64, 0, len(s.Records))
	for seq, e := range s.Records {
		if k := e.Record.String("kind"); e.Record.String("scope") == "batch" && (k == "retry" || k == "resend") {
			seqs = append(seqs, seq)
		}
	}
	slices.Sort(seqs)
	for _, seq := range seqs {
		e := s.Records[seq]
		b := s.Batches[e.Record.String("batch")]
		if b == nil {
			continue
		}
		var roots []string
		for m := range b.Members {
			if !slices.Contains(roots, m.Root) {
				roots = append(roots, m.Root)
			}
		}
		slices.Sort(roots)
		t := b.Tries[e.Record.Int("try")]
		r.add(checkValueKindLanding, roots, e.At, t != nil && t.Verify.Record.String("outcome") == "verified", false)
	}
}

// checkValueAnswers: the Owner's answers on cards (not a rule's). Caught is
// an answer other than the option the card recommended, or any answer on a
// card that recommended none: what a rule taking the recommendation would
// not have given. An answer before the first recorded recommendation says
// nothing of it.
func checkValueAnswers(g checkValueGrid, rows checkValueRows) {
	g.all("owner_cards", "an answer on a card is not told noise", "noise")
	g.all("owner_cards", "the records keep the Owner's time on a card, not a machine's", "minutes")
	for _, ans := range rows.answers {
		if strings.HasPrefix(ans.By, "rule ") {
			continue
		}
		a := g.at("owner_cards", ans.At)
		if a == nil {
			continue
		}
		a.ran++
		a.ownerMinutes += checkValueOwnerMinutes
		a.unknownAs(fmt.Sprintf("estimated: %d minutes per answer", checkValueOwnerMinutes), "owner_minutes_basis")
		if rows.recommendedSince == nil || ans.At.Before(*rows.recommendedSince) {
			a.unknownAs(checkValueRecordedFrom, "caught")
			continue
		}
		if !ans.HasRecommended || ans.Option != ans.Recommended {
			a.caught++
			a.unique++
		}
	}
}

// checkValueNewRounds: a task sent back for a new round is saved when it
// was later accepted; its minutes are the new round's attempts', from the
// rework until the next.
func checkValueNewRounds(g checkValueGrid, rows checkValueRows, r *checkValueRetries) {
	for _, w := range rows.reworks {
		r.add(checkValueKindRound, []string{w.Task}, w.At, rows.tasks[w.Task].State == "accepted", false)
		a := g.at("new_round", w.At)
		if a == nil {
			continue
		}
		end := time.Time{}
		for _, next := range r.rounds[w.Task] {
			if next.After(w.At) {
				end = next
				break
			}
		}
		for _, at := range rows.attempts[w.Task] {
			if at.Started == nil || at.Started.Before(w.At) || !end.IsZero() && !at.Started.Before(end) {
				continue
			}
			if at.Ended == nil {
				a.unknownAs("an attempt of the round has not ended", "minutes")
				continue
			}
			a.seconds += at.Ended.Sub(*at.Started).Seconds()
		}
	}
}

// The kinds of retry, one row each.
const (
	checkValueKindRound   = "new_round"
	checkValueKindLanding = "landing_retry"
	checkValueKindBudget  = "budget_grant"
)

// checkValueRetry is one retry: of tasks, at, saved or not, by the Owner.
type checkValueRetry struct {
	kind         string
	tasks        []string
	at           time.Time
	saved, owner bool
}

// checkValueRetries are the retries, and each task's reworks, which split
// its rounds.
type checkValueRetries struct {
	list   []checkValueRetry
	rounds map[string][]time.Time
}

func (r *checkValueRetries) add(kind string, tasks []string, at time.Time, saved, owner bool) {
	r.list = append(r.list, checkValueRetry{kind: kind, tasks: tasks, at: at, saved: saved, owner: owner})
}

// round is task's round at t: how many of its reworks came by then.
func (r *checkValueRetries) round(task string, t time.Time) int {
	n := 0
	for _, at := range r.rounds[task] {
		if !at.After(t) {
			n++
		}
	}
	return n
}

// fold adds the retries up: a save is unique when no other kind of retry
// was used on the same task in that round.
func (r *checkValueRetries) fold(g checkValueGrid) {
	for _, key := range []string{checkValueKindRound, checkValueKindLanding, checkValueKindBudget} {
		g.all(key, "a retry is not told noise", "noise")
	}
	g.all(checkValueKindLanding, "a retry's time is its check's, counted in that row", "minutes")
	g.all(checkValueKindBudget, "a grant's time is the attempts it allows, counted in their rows", "minutes")
	for i := range g.rows[checkValueKindBudget] {
		g.rows[checkValueKindBudget][i].unknownAs(fmt.Sprintf("estimated: %d minutes per grant the Owner gave", checkValueOwnerMinutes), "owner_minutes_basis")
	}
	for _, x := range r.list {
		a := g.at(x.kind, x.at)
		if a == nil {
			continue
		}
		a.ran++
		if x.owner {
			a.ownerMinutes += checkValueOwnerMinutes
		}
		if !x.saved {
			continue
		}
		a.caught++
		unique := true
		for _, y := range r.list {
			if y.kind == x.kind || !unique {
				continue
			}
			for _, t := range x.tasks {
				if slices.Contains(y.tasks, t) && r.round(t, y.at) == r.round(t, x.at) {
					unique = false
				}
			}
		}
		if unique {
			a.unique++
		}
	}
}

// CheckValueNote is a check_value note's body: the controller proposed the
// goal of mission to delete the row key, marked in week.
type CheckValueNote struct {
	Key     string `json:"key"`
	Week    string `json:"week"`
	Mission string `json:"mission"`
}

// NoteCheckValue records text and n on the controller's stream.
func NoteCheckValue(ctx context.Context, db DB, text string, n CheckValueNote) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: ControllerStream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": text, "check_value": map[string]any{"key": n.Key, "week": n.Week, "mission": n.Mission}}})
		return err
	})
}

// CheckValueNoted says whether row key has its note for week.
func CheckValueNoted(ctx context.Context, db DB, key, week string) (bool, error) {
	var noted bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
		  AND actor_id = 'controller' AND body_json->'check_value'->>'key' = $2 AND body_json->'check_value'->>'week' = $3)`,
			ControllerStream, key, week).Scan(&noted)
	})
	return noted, err
}

// CheckValueOpenGoal is the mission of the latest check_value note of row
// key that is still open (not done, stopped or expired); "" for none.
func CheckValueOpenGoal(ctx context.Context, db DB, key string) (string, error) {
	var mission string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT m.mission_id FROM agentrt.event e
		  JOIN agentrt.mission m ON m.mission_id = e.body_json->'check_value'->>'mission'
		  WHERE e.event_stream_id = $1 AND e.event_type = 'note' AND e.actor_id = 'controller'
		    AND e.body_json->'check_value'->>'key' = $2 AND m.state NOT IN ('done', 'stopped', 'expired')
		  ORDER BY e.seq DESC LIMIT 1`, ControllerStream, key).Scan(&mission)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return mission, err
}
