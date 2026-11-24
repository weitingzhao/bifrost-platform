package evalreadings

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The ledger readings. The ledger is read only through eventlog's functions
// (LatestByKey, OfType): this package writes no SQL on it, and the medians
// and ratios are computed here.

func ledgerReadings(ctx context.Context, db eventlog.DB, tasks FailedTasks, w Window) ([]Reading, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	src := ledgerSource{
		latestByKey: func(typ, key string, since time.Time) ([]eventlog.Row, error) {
			return eventlog.LatestByKey(ctx, tx, typ, key, since)
		},
		ofType: func(typ string, from, to time.Time) ([]eventlog.Row, error) {
			return eventlog.OfType(ctx, tx, typ, from, to)
		},
	}
	src.asks = func(from, to time.Time) ([]askRow, error) { return readAsks(ctx, tx, from, to) }
	if tasks != nil {
		src.tasks = func(repo, commit, run string) ([]string, error) { return tasks(ctx, repo, commit, run) }
	}
	return src.readings(w)
}

// ledgerSource is what the readings ask of the ledger; tests give it rows.
type ledgerSource struct {
	latestByKey func(typ, key string, since time.Time) ([]eventlog.Row, error)
	ofType      func(typ string, from, to time.Time) ([]eventlog.Row, error)
	// tasks reads a failed run's tasks for the CI failure reading; nil leaves
	// every failed run's task unrecorded.
	tasks func(repo, commit, run string) ([]string, error)
	// asks reads the asks between seats and their answers; nil leaves the
	// ask readings unmeasured.
	asks func(from, to time.Time) ([]askRow, error)
}

func (s ledgerSource) readings(w Window) ([]Reading, error) {
	var out []Reading
	runs, err := s.latestByKey("pipeline_seen", "uid", w.From)
	if err != nil {
		return nil, fmt.Errorf("pipeline_seen: %w", err)
	}
	out = append(out, ciReadings(runs, w)...)
	out = append(out, ciFailureReadings(runs, s.tasks, w)...)
	out = append(out, deliveryReading(runs, w))
	owner, err := s.ownerReadings(w)
	if err != nil {
		return nil, err
	}
	out = append(out, owner...)
	blocks, err := s.guardBlockReading(w)
	if err != nil {
		return nil, err
	}
	out = append(out, blocks)
	asks, err := s.askWaitReadings(w)
	if err != nil {
		return nil, err
	}
	out = append(out, asks...)
	deploys, err := s.deployReadings(w)
	if err != nil {
		return nil, err
	}
	return append(out, deploys...), nil
}

func str(b map[string]any, k string) string {
	if v, ok := b[k].(string); ok {
		return v
	}
	return ""
}

func instant(b map[string]any, k string) (time.Time, bool) {
	v := str(b, k)
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	return t, err == nil
}

func in(t time.Time, w Window) bool { return !t.Before(w.From) && t.Before(w.To) }

// median is percentile_cont(0.5): the middle value, or the mean of the two
// middle values.
func median(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := len(s) / 2
	v := s[m]
	if len(s)%2 == 0 {
		v = (s[m-1] + s[m]) / 2
	}
	return &v
}

type runStats struct {
	runs, ok int
	run      []float64
}

// completedRuns groups the runs of a kind that completed in the window by
// pipeline. A run whose times do not parse is left out.
func completedRuns(rows []eventlog.Row, kind string, w Window) map[string]*runStats {
	by := map[string]*runStats{}
	for _, r := range rows {
		b := r.Body
		if str(b, "kind") != kind {
			continue
		}
		started, ok2 := instant(b, "started_at")
		done, ok3 := instant(b, "completed_at")
		if !ok2 || !ok3 || !in(done, w) {
			continue
		}
		p := str(b, "pipeline")
		st := by[p]
		if st == nil {
			st = &runStats{}
			by[p] = st
		}
		st.runs++
		if str(b, "result") == "Succeeded" {
			st.ok++
		}
		st.run = append(st.run, done.Sub(started).Seconds())
	}
	return by
}

// ciReadings: kind=ci runs (ci-*), one set per pipeline. Run time is
// started_at to completed_at. Landing runs (bifrost-land) are not here: the
// ledger records them as another kind, and the CI line ruled on 2026-11-24
// that they stay a class of their own, since most land failures are refusals
// (no CI run, a rebase conflict, the freeze chain), not red tests, and would
// read the CI failure rate high. The queue is read from Prometheus
// (promReadings, ciQueueReadings) and does count land-* pods.
func ciReadings(rows []eventlog.Row, w Window) []Reading {
	src := "event ledger pipeline_seen kind=ci, latest fact per run uid, completed in window"
	by := completedRuns(rows, "ci", w)
	if len(by) == 0 {
		return []Reading{finish(Reading{Name: "ci_run_p50_seconds", Unit: "seconds", Window: w, Definition: def(src, "median of completed_at - started_at")}, true)}
	}
	names := make([]string, 0, len(by))
	for p := range by {
		names = append(names, p)
	}
	sort.Strings(names)
	var out []Reading
	for _, p := range names {
		st := by[p]
		l := map[string]string{"pipeline": p}
		out = append(out,
			finish(Reading{Name: "ci_run_p50_seconds", Labels: l, Value: median(st.run), Unit: "seconds", Samples: st.runs, Window: w, Definition: def(src, "median of completed_at - started_at")}, true),
			finish(Reading{Name: "ci_success_ratio", Labels: l, Value: ratio(st.ok, st.runs), Unit: "ratio", Samples: st.runs, Window: w, Definition: def(src, "runs with result Succeeded / runs")}, false),
		)
	}
	return out
}

// deliveryReading: kind=delivery runs, all pipelines together.
func deliveryReading(rows []eventlog.Row, w Window) Reading {
	runs, ok := 0, 0
	for _, st := range completedRuns(rows, "delivery", w) {
		runs += st.runs
		ok += st.ok
	}
	src := "event ledger pipeline_seen kind=delivery, latest fact per run uid, completed in window"
	return finish(Reading{Name: "delivery_success_ratio", Value: ratio(ok, runs), Unit: "ratio", Samples: runs, Window: w, Definition: def(src, "runs with result Succeeded / runs")}, false)
}

// ownerReadings: what the Owner did that the system recorded. Answers he
// gives in a thread are not recorded by the platform and are not counted
// (the question tool has had no new question since 2026-11-12; the
// communication line checked, 2026-11-20).
func (s ledgerSource) ownerReadings(w Window) ([]Reading, error) {
	// approval_seen and question_seen are re-recorded on each pass; a fact
	// decided in the window may have been first seen long before it.
	approvals, err := s.latestByKey("approval_seen", "approval", time.Time{})
	if err != nil {
		return nil, fmt.Errorf("approval_seen: %w", err)
	}
	var waits []float64
	for _, r := range approvals {
		created, ok1 := instant(r.Body, "created_at")
		decided, ok2 := instant(r.Body, "decided_at")
		if ok1 && ok2 && in(decided, w) {
			waits = append(waits, decided.Sub(created).Seconds())
		}
	}
	questions, err := s.latestByKey("question_seen", "question", time.Time{})
	if err != nil {
		return nil, fmt.Errorf("question_seen: %w", err)
	}
	answered := 0
	for _, r := range questions {
		if t, ok := instant(r.Body, "answered_at"); ok && in(t, w) {
			answered++
		}
	}
	decided := len(waits)
	actions := finish(Reading{Name: "owner_actions", Value: count(decided + answered), Unit: "count", Samples: decided + answered, Window: w,
		Note:       "excludes answers given in threads, which the platform does not record",
		Labels:     map[string]string{"approvals_decided": fmt.Sprint(decided), "questions_answered": fmt.Sprint(answered)},
		Definition: def("event ledger approval_seen and question_seen, latest fact per approval / question", "approvals decided in window + questions answered in window")}, false)
	measuredZero(&actions)
	interruptions, err := s.interruptionReading(w)
	if err != nil {
		return nil, err
	}
	privilege, err := s.privilegeReading(w)
	if err != nil {
		return nil, err
	}
	return []Reading{
		actions,
		interruptions,
		privilege,
		finish(Reading{Name: "owner_wait_p50_seconds", Value: median(waits), Unit: "seconds", Samples: decided, Window: w,
			Definition: def("event ledger approval_seen, latest fact per approval", "median of decided_at - created_at over approvals decided in window")}, true),
	}, nil
}

// InterruptionsDefinitionVersion is owner_interruptions' own version. 2
// (2026-11-23): the label "failed" read as "the push did not arrive" to the
// coordinator; it counts pushes saying an approved card failed to run, so it
// is now card_failed, and by_kind splits the counted pushes (created,
// reminder_waiting, unknown, ...).
const InterruptionsDefinitionVersion = "2"

// notifiedSince is when the ledger began keeping the pushes to the Owner's
// phone: the first PROD release with approval.card.notified (2026-11-20
// 16:20:43Z, bifrost-deliver-platform-prod-1795191637, 7c4db486). A window
// that starts earlier would count only part of it.
var notifiedSince = time.Date(2026, 11, 20, 16, 20, 43, 0, time.UTC)

// interruptionReading: owner_interruptions, the pushes about a card that
// reached the Owner's phone in the window (approval.card.notified, any kind
// but failed). Origin: the metrics line, 2026-11-20 ("relax cards into
// records", law 91): how often he is interrupted had no system record across
// the change; the comm line put the pushes on the ledger.
func (s ledgerSource) interruptionReading(w Window) (Reading, error) {
	const name = "owner_interruptions"
	d := def("event ledger approval.card.notified (one per push about a card: created, reminder, result)",
		"pushes in the window whose kind is not failed; labels: card_failed = pushes saying an approved card failed to run, by_kind = the counted pushes by kind")
	d.Version = InterruptionsDefinitionVersion
	if w.From.Before(notifiedSince) {
		return finish(Reading{Name: name, Unit: "count", Window: w, Definition: d,
			Note: "the ledger keeps the pushes only since " + notifiedSince.Format(time.RFC3339) + "; this window starts earlier"}, false), nil
	}
	rows, err := s.ofType("approval.card.notified", w.From, w.To)
	if err != nil {
		return Reading{}, fmt.Errorf("approval.card.notified: %w", err)
	}
	sent, failed := 0, 0
	kinds := map[string]int{}
	for _, r := range rows {
		k := str(r.Body, "kind")
		if k == "failed" {
			failed++
			continue
		}
		sent++
		kinds[k]++
	}
	labels := map[string]string{"card_failed": fmt.Sprint(failed)}
	if len(kinds) > 0 {
		names := make([]string, 0, len(kinds))
		for k := range kinds {
			names = append(names, k)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, k := range names {
			parts = append(parts, fmt.Sprintf("%s %d", k, kinds[k]))
		}
		labels["by_kind"] = strings.Join(parts, ", ")
	}
	r := finish(Reading{Name: name, Value: count(sent), Unit: "count", Samples: sent, Window: w, Definition: d,
		Labels: labels}, false)
	measuredZero(&r)
	return r, nil
}

// privilegeSince is when the ledger began keeping the Owner's privilege uses
// (owner.privilege.used): the PROD release of its producer (bus/privilege),
// as the communication line reported it. Windows that start earlier are
// unmeasured, never a zero nobody counted. Zero would mean no producer.
var privilegeSince = time.Date(2026, 11, 21, 21, 6, 32, 0, time.UTC)

// privilegeReading: owner_privilege_uses, the times in the window the Owner
// stepped in to move something that was stuck (lines waiting on each other, or
// nothing saying whose it is). Origin: law 91 (the Owner, 2026-11-21, relayed):
// the privilege must exist, its uses should fall, and the count is the fixed
// reference for every before-and-after. The facts are the coordinator's, the
// definition Audit's; this only counts what the ledger holds.
func (s ledgerSource) privilegeReading(w Window) (Reading, error) {
	const name = "owner_privilege_uses"
	d := def("event ledger owner.privilege.used (one per time the Owner stepped in)",
		"events in the window")
	if privilegeSince.IsZero() {
		return finish(Reading{Name: name, Unit: "count", Window: w, Definition: d,
			Note: "no producer of owner.privilege.used yet; the uses are only in a hand-kept note, which is not a system record (law 93)"}, false), nil
	}
	if w.From.Before(privilegeSince) {
		return finish(Reading{Name: name, Unit: "count", Window: w, Definition: d,
			Note: "the ledger keeps privilege uses only since " + privilegeSince.Format(time.RFC3339) + "; this window starts earlier"}, false), nil
	}
	rows, err := s.ofType("owner.privilege.used", w.From, w.To)
	if err != nil {
		return Reading{}, fmt.Errorf("owner.privilege.used: %w", err)
	}
	r := finish(Reading{Name: name, Value: count(len(rows)), Unit: "count", Samples: len(rows), Window: w, Definition: d}, false)
	measuredZero(&r)
	return r, nil
}

// mainLookback is how far before the window a commit's arrival on main is
// looked for; a commit older than that is not timed.
const mainLookback = 30 * 24 * time.Hour

// deployReadings: successful prod deploys, and for each deployed commit the
// time from reaching main to its first prod deploy. Only the commit a deploy
// names is timed (the commits it carries along are not traced through git
// ancestry in version 1).
func (s ledgerSource) deployReadings(w Window) ([]Reading, error) {
	rows, err := s.ofType("release.deploy.finished", w.From, w.To)
	if err != nil {
		return nil, fmt.Errorf("release.deploy.finished: %w", err)
	}
	type key struct{ repo, sha string }
	firstDeploy := map[key]time.Time{}
	byRepo := map[string]int{}
	total := 0
	for _, r := range rows {
		b := r.Body
		if str(b, "env") != "prod" || str(b, "change") != "succeeded" {
			continue
		}
		total++
		byRepo[str(b, "repo")]++
		k := key{str(b, "repo"), str(b, "sha")}
		if t, ok := firstDeploy[k]; !ok || r.At.Before(t) {
			firstDeploy[k] = r.At
		}
	}
	src := "event ledger release.deploy.finished env=prod change=succeeded"
	labels := map[string]string{}
	for repo, n := range byRepo {
		labels[repo] = fmt.Sprint(n)
	}
	deploys := finish(Reading{Name: "prod_deploys", Value: count(total), Unit: "count", Samples: total, Labels: labels, Window: w,
		Definition: def(src, "successful prod deploys finished in window; labels give the count per repository")}, false)
	measuredZero(&deploys)

	mains, err := s.ofType("git.main.advanced", w.From.Add(-mainLookback), w.To)
	if err != nil {
		return nil, fmt.Errorf("git.main.advanced: %w", err)
	}
	reached := map[key]time.Time{}
	for _, r := range mains {
		k := key{str(r.Body, "repo"), str(r.Body, "commit")}
		if t, ok := reached[k]; !ok || r.At.Before(t) {
			reached[k] = r.At
		}
	}
	var leads []float64
	for k, d := range firstDeploy {
		if m, ok := reached[k]; ok && !d.Before(m) {
			leads = append(leads, d.Sub(m).Seconds())
		}
	}
	lead := finish(Reading{Name: "main_to_prod_p50_seconds", Value: median(leads), Unit: "seconds", Samples: len(leads), Window: w,
		Definition: def("event ledger git.main.advanced and "+src, "median of (first prod deploy of a commit) - (commit reached main), deployed commits only, main looked up 30 days back"),
		Note:       "version 1 times only the commit a deploy names; the commits it carries along are not traced"}, true)
	return []Reading{deploys, lead}, nil
}
