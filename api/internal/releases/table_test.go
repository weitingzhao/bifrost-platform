package releases

import (
	"context"
	"errors"
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
