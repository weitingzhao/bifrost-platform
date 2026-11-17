package releases

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
}

func (f *fakeRows) Records(context.Context) ([]Record, error) { return append([]Record(nil), f.rows...), nil }

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

// 2026-11-17, step 2 of moving the release records into the database (with
// the communication layer's bus step 3): before Current reads the table,
// each pass copies the ConfigMap records the table lacks (those from before
// 12:27, when rows started) and asks Current both ways; a lane whose two
// answers differ is counted and named, and readers still get the ConfigMaps'.
// Positive control: without the Backfill loop the first check fails
// (backfilled 0, and the rows answer "no deployment").
// The second control: comparing nothing (Differ never counted) fails the last check.
func TestTickBackfillsTheTableAndComparesCurrentBothWays(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s, _ := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
		pipelineRun("deliver-stg-2", "deliver-stg", true, t1.Add(time.Hour), nil, nil),
		cloneTaskRun("deliver-stg-2", "clone-core", "core", shaC),
	)
	if r := s.Tick(ctx); r.Recorded != 2 || r.Compare != nil {
		t.Fatalf("without a reader nothing is compared: %+v", r)
	}

	rows := &fakeRows{}
	s.SetTableReader(rows)
	r := s.Tick(ctx)
	c := r.Compare
	if c == nil || c.Backfilled != 2 || c.Compared != 2 || c.Differ != 0 || c.Err != "" {
		t.Fatalf("compare = %+v", c)
	}
	if got, err := s.Current(ctx, "app", "stg"); err != nil || got.Run != "deliver-stg-2" {
		t.Fatalf("readers still get the ConfigMaps' answer: %+v %v", got, err)
	}
	if again := s.Tick(ctx).Compare; again.Backfilled != 0 || again.Differ != 0 {
		t.Fatalf("a run with a row is not copied again: %+v", again)
	}

	// The table lacks the newest deployment: from the rows its run is a later
	// delivery than the newest row, so unsettled (never the older one); the
	// two answers differ, by name.
	short := &fakeRows{refuse: map[string]bool{"deliver-stg-2": true}}
	s.SetTableReader(short)
	c = s.Tick(ctx).Compare
	if c.Differ != 1 || len(c.Diffs) != 1 || !strings.Contains(c.Diffs[0], "app/stg: configmaps run deliver-stg-2, rows unsettled") {
		t.Fatalf("compare = %+v", c)
	}
}
