package evalreadings

import (
	"context"
	"fmt"
	"sort"
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

// ciReadings: kind=ci runs (ci-* and land-*, the CI line confirmed
// 2026-11-20), one set per pipeline. Run time is started_at to completed_at,
// The queue is read from Prometheus (promReadings, ciQueueReadings).
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
	return []Reading{
		actions,
		finish(Reading{Name: "owner_wait_p50_seconds", Value: median(waits), Unit: "seconds", Samples: decided, Window: w,
			Definition: def("event ledger approval_seen, latest fact per approval", "median of decided_at - created_at over approvals decided in window")}, true),
	}, nil
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
