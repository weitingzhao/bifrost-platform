package releases

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// 2026-11-17, step 1 of moving the release records into the database (0027):
// every record the recorder writes as a ConfigMap is also handed to the
// table writer, once, as a successful run; a row that fails is an error of
// the pass and never stops the ConfigMap. A run already recorded is not
// handed again: a row that failed waits for the comparison of rows with
// ConfigMaps (the next step), which writes the rows that are missing.
// Positive control: without the writeRow call in Tick, rows stays empty and
// the first check fails.
func TestTickHandsEachRecordToTheTableOnce(t *testing.T) {
	t1 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s, core := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
		pipelineRun("deliver-stg-2", "deliver-stg", true, t1.Add(time.Hour), nil, nil),
		cloneTaskRun("deliver-stg-2", "clone-core", "core", shaC),
		pipelineRun("deliver-stg-3", "deliver-stg", false, t1, nil, nil),
	)
	var rows []string
	s.SetTable(func(_ context.Context, rec Record, succeeded bool) error {
		if !succeeded {
			t.Errorf("%s handed as failed", rec.Run)
		}
		rows = append(rows, rec.Run)
		if rec.Run == "deliver-stg-2" {
			return errors.New("database down")
		}
		return nil
	})
	r := s.Tick(context.Background())
	if len(rows) != 2 || r.Recorded != 2 || r.Rows != 1 || len(r.Errors) != 1 {
		t.Fatalf("rows=%v tick=%+v", rows, r)
	}
	if _, err := core.CoreV1().ConfigMaps("cicd").Get(context.Background(), "release-deliver-stg-2", metav1.GetOptions{}); err != nil {
		t.Fatal("the row's failure stopped the ConfigMap:", err)
	}
	if again := s.Tick(context.Background()); again.Rows != 0 || len(rows) != 2 {
		t.Fatalf("a recorded run was handed again: %+v %v", again, rows)
	}
}

func TestAnnouncedNamesTheRepositoryOfTheRevision(t *testing.T) {
	two := map[string]RepoBuild{"bifrost-platform": {SHA: shaA}, "bifrost-ui": {SHA: shaC}}
	for _, c := range []struct {
		name string
		rec  Record
		repo string
		ok   bool
	}{
		{"one repository built the revision", Record{Revision: shaA, Repos: two}, "bifrost-platform", true},
		{"no revision", Record{Repos: two}, "", false},
		{"no repository built it", Record{Revision: shaB, Repos: two}, "", false},
		{"two built it: which one is not known", Record{Revision: shaA, Repos: map[string]RepoBuild{"a": {SHA: shaA}, "b": {SHA: shaA}}}, "", false},
	} {
		repo, sha, ok := Announced(c.rec)
		if repo != c.repo || ok != c.ok || (ok && sha != c.rec.Revision) {
			t.Errorf("%s: %q %q %v", c.name, repo, sha, ok)
		}
	}
}

// fakeRows is a record table in memory; refuse names runs Backfill skips.
type fakeRows struct {
	rows   []Record
	refuse map[string]bool
	err    error
}

func (f *fakeRows) Records(context.Context) ([]Record, error) {
	return append([]Record(nil), f.rows...), nil
}

func (f *fakeRows) Deployments(_ context.Context, lane, env string) ([]Record, error) {
	var out []Record
	for _, r := range f.rows {
		if r.Deploys && r.Lane == lane && r.Env == env {
			out = append(out, r)
		}
	}
	return out, f.err
}

func (f *fakeRows) MissingByRun(_ context.Context, runs []string) (map[string]int, error) {
	out := map[string]int{}
	for _, want := range runs {
		for _, r := range f.rows {
			if r.Run == want {
				out[want] = len(r.Missing)
			}
		}
	}
	return out, f.err
}

func (f *fakeRows) Backfill(_ context.Context, rec Record) (bool, error) {
	for _, r := range f.rows {
		if r.Run == rec.Run {
			return false, nil
		}
	}
	if f.refuse[rec.Run] {
		return false, nil
	}
	f.rows = append(f.rows, rec)
	return true, nil
}

// 2026-11-18, the records move to the table (all 1,338 ConfigMap records had
// matched their rows): the first pass copies what the table lacks, later passes
// neither read nor write ConfigMaps, and a new run becomes a row only.
// Positive control: leaving the ConfigMap write in tickRows fails the check that
// the new run has no ConfigMap; dropping copyMaps fails the first check.
func TestTickKeepsTheRecordsInTheTable(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s, core := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
	)
	if r := s.Tick(ctx); r.Recorded != 1 {
		t.Fatalf("without a table the ConfigMap is the record: %+v", r)
	}
	rows := &fakeRows{}
	s.SetTableReader(rows)
	var written []string
	s.SetTable(func(_ context.Context, rec Record, _ bool) error {
		written = append(written, rec.Run)
		rows.rows = append(rows.rows, rec)
		return nil
	})
	if r := s.Tick(ctx); r.Backfilled != 1 || r.Recorded != 0 || len(written) != 0 || len(r.Errors) != 0 {
		t.Fatalf("first pass: %+v written %v", r, written)
	}
	dyn := s.clients
	_, d, _ := dyn()
	run := pipelineRun("deliver-stg-2", "deliver-stg", true, t1.Add(time.Hour), nil, nil)
	if _, err := d.Resource(pipelineRunGVR).Namespace("cicd").Create(ctx, run, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	core.ClearActions()
	if r := s.Tick(ctx); r.Recorded != 1 || r.Rows != 1 || len(written) != 1 || written[0] != "deliver-stg-2" {
		t.Fatalf("new run: %+v written %v", r, written)
	}
	if got := core.Actions(); len(got) != 0 {
		t.Fatalf("a pass over the table touched ConfigMaps: %v", got)
	}
	if again := s.Tick(ctx); again.Recorded != 0 || len(written) != 1 {
		t.Fatalf("a recorded run was written again: %+v", again)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 2 || list[0].Run != "deliver-stg-2" {
		t.Fatalf("list from the rows, newest first: %+v %v", list, err)
	}
}

// Same origin: STG keeps its own table from the shared runs and announces
// nothing; release events are PROD's. Positive control: using the writer in
// quiet mode fails the check that it was not called.
func TestAQuietRecorderWritesRowsWithoutAnnouncing(t *testing.T) {
	t.Setenv("PLATFORM_RELEASE_RECORDER", "rows")
	if !RecorderWanted() || !RecorderQuiet() {
		t.Fatal("rows is a recorder, quiet")
	}
	s, _ := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC), nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
	)
	rows := &fakeRows{}
	s.SetTableReader(rows)
	s.SetTable(func(context.Context, Record, bool) error { t.Error("announced"); return nil })
	if r := s.Tick(context.Background()); r.Recorded != 1 || len(rows.rows) != 1 {
		t.Fatalf("tick = %+v rows %v", r, rows.rows)
	}
}

// Same origin: a row that could not read a source is written again only when
// the run now gives more. Positive control: dropping the Missing comparison
// writes a row on every pass and fails the count.
func TestARowMissingASourceIsRebuiltOnlyWhenItImproves(t *testing.T) {
	s, _ := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC), nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
	)
	rows := &fakeRows{rows: []Record{{Run: "deliver-stg-1", Missing: []string{"clone-core"}}}}
	s.SetTableReader(rows)
	writes := 0
	s.SetTable(func(_ context.Context, rec Record, _ bool) error {
		writes++
		rows.rows = []Record{rec}
		return nil
	})
	s.Tick(context.Background())
	s.Tick(context.Background())
	if writes != 1 {
		t.Fatalf("writes = %d", writes)
	}
}

// 2026-11-18: Current reads the rows once they are wired (all 1,272 records
// matched their ConfigMaps that night), so a reader no longer depends on the
// ConfigMaps being there, and a table it cannot read is an error, never the
// older source's answer passed off as the table's.
// Positive control: Current reading the ConfigMaps fails the first check,
// where they are gone.
func TestCurrentReadsTheTable(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC)
	s, core := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
	)
	s.Tick(ctx)
	rows := &fakeRows{}
	s.SetTableReader(rows)
	s.Tick(ctx)
	if err := core.CoreV1().ConfigMaps("cicd").Delete(ctx, "release-deliver-stg-1", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Current(ctx, "app", "stg"); err != nil || got.Run != "deliver-stg-1" || got.Repos["core"].SHA != shaA {
		t.Fatalf("current = %+v %v", got, err)
	}
	rows.err = errors.New("database down")
	if _, err := s.Current(ctx, "app", "stg"); err == nil || !strings.Contains(err.Error(), "database down") {
		t.Fatalf("an unreadable table = %v", err)
	}
	rows.err = nil
	rows.rows[0].CompletedAt = time.Time{}
	if _, err := s.Current(ctx, "app", "stg"); !errors.Is(err, ErrUnsettled) {
		t.Fatalf("a row that is no deployment = %v", err)
	}
}

// 2026-11-18: ac7ece90 on STG. Only PROD's workers write records, so STG's
// table is empty, and Current answered "no deployment" for an environment the
// ConfigMaps say is deployed.
// Positive control: returning currentFromTable's answer alone fails this.
func TestCurrentWithAnEmptyTableReadsTheConfigMaps(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC)
	s, _ := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
	)
	s.Tick(ctx)
	s.SetTableReader(&fakeRows{refuse: map[string]bool{"deliver-stg-1": true}})
	if got, err := s.Current(ctx, "app", "stg"); err != nil || got.Run != "deliver-stg-1" {
		t.Fatalf("current = %+v %v", got, err)
	}
	if _, err := s.Current(ctx, "app", "prod"); !errors.Is(err, ErrNoDeployment) {
		t.Fatalf("nothing recorded anywhere = %v", err)
	}
}

// Same origin: the record carries the run's cause label, so the recorder's
// release.deploy.finished can name the event that woke the start.
func TestARecordCarriesTheRunsCause(t *testing.T) {
	t1 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	run := pipelineRun("deliver-stg-9", "deliver-stg", true, t1, nil, map[string]string{"bifrost.io/cause": "ev-77"})
	rule := Rule{Lane: "app", Env: "stg", Deploys: true}
	if rec := buildRecord(run, rule, nil, t1); rec.Cause != "ev-77" {
		t.Fatalf("cause = %q", rec.Cause)
	}
	if rec := buildRecord(pipelineRun("deliver-stg-8", "deliver-stg", true, t1, nil, nil), rule, nil, t1); rec.Cause != "" {
		t.Fatalf("a run with no label named %q", rec.Cause)
	}
}

// slimRun is a run as the bus run cache hands it back: no params, no results,
// and the labels it keeps.
func slimRun(run *unstructured.Unstructured) unstructured.Unstructured {
	out := run.DeepCopy()
	unstructured.RemoveNestedField(out.Object, "spec", "params")
	unstructured.RemoveNestedField(out.Object, "status", "results")
	out.SetAnnotations(nil)
	return *out
}

// 2026-11-18 (bus/RULES.md §10 R3; the recorder and Current were the largest
// lists of runs left): both read runs through the compared cache, on their own
// routes, and the record is built from the run read by name, because the
// cache keeps only part of one.
// Positive control: buildRecord over the listed run instead of the fetched
// one leaves Revision empty and fails the check on it.
func TestRecorderAndCurrentReadRunsThroughTheReader(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC)
	run := pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil)
	_ = unstructured.SetNestedSlice(run.Object, []any{map[string]any{"name": "revision", "value": shaA}}, "spec", "params")
	s, _ := newFakeService(t, run, cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA))
	routes := map[string]int{}
	s.SetRunReader(func(route string, pipelines []string, _ func(*unstructured.Unstructured) bool, listed func() ([]unstructured.Unstructured, error)) ([]unstructured.Unstructured, error) {
		routes[route]++
		items, err := listed()
		if err != nil {
			return nil, err
		}
		out := make([]unstructured.Unstructured, 0, len(items))
		for i := range items {
			out = append(out, slimRun(&items[i]))
		}
		return out, nil
	})
	if r := s.Tick(ctx); r.Recorded != 1 || len(r.Errors) != 0 {
		t.Fatalf("tick = %+v", r)
	}
	got, err := s.Current(ctx, "app", "stg")
	if err != nil || got.Run != "deliver-stg-1" || got.Revision != shaA {
		t.Fatalf("current = %+v %v", got, err)
	}
	if routes[RouteTick] != 1 || routes[RouteCurrent] != 1 {
		t.Fatalf("routes = %v", routes)
	}
}
