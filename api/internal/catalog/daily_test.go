package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// 2026-11-24, the map line's completion criteria in the plan (R6:6.9): the
// read routes' daily calls, how long a new commit waits to be on the map and
// the pool's oldest entry were read by hand when asked, so nothing could say
// they held from day to day. Each round keeps today's point; a later round
// of the same day replaces it, a new day adds one. The control: an unchanged
// main head times nothing.
func TestDailyPointsKeepReadsWaitsAndTheOldestEntry(t *testing.T) {
	day := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	s := &Scanner{Kept: memKept{}, CommitTime: func(_ context.Context, repo, commit string) (time.Time, error) {
		if commit == "bad" {
			return time.Time{}, errors.New("gitea down")
		}
		return day.Add(-7 * time.Minute), nil
	}}
	s.noteSighting(context.Background(), "p", "a", "a", day)
	s.noteSighting(context.Background(), "p", "", "b", day)
	s.noteSighting(context.Background(), "p", "a", "bad", day)
	s.noteSighting(context.Background(), "p", "a", "b", day)
	repos, _ := json.Marshal([]string{"p", "q"})
	_ = s.kept().Write(keptRepos, repos)
	three, five := 3, 5
	s.keep(t, "p", &three)
	s.keep(t, "q", &five)
	in := StatusInputs{Requests: func(_ context.Context, routes []string, w time.Duration) (map[string]RouteCount, error) {
		if w != 24*time.Hour {
			t.Errorf("window %v", w)
		}
		return map[string]RouteCount{"/api/v1/catalog/status": {Total: 1754.6}}, nil
	}}
	got := s.keepDaily(context.Background(), in, day)
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	p := got[0]
	if p.Date != "2026-11-24" || p.Reads["/api/v1/catalog/status"] != 1755 || p.Reads["/api/v1/catalog/domain"] != 0 {
		t.Errorf("reads %+v", p)
	}
	if p.MaxOldestDays != 5 || p.OldestDays["p"] != 3 || p.Seen != 1 || p.MaxLagSeconds != 420 || p.RoundSeconds != int64(Every/time.Second) {
		t.Errorf("point %+v", p)
	}
	if again := s.keepDaily(context.Background(), StatusInputs{}, day.Add(time.Hour)); len(again) != 1 || again[0].Reads != nil {
		t.Errorf("a later round the same day replaces the point, unread counts are nil: %+v", again)
	}
	if next := s.keepDaily(context.Background(), StatusInputs{}, day.Add(24*time.Hour)); len(next) != 2 || next[1].Seen != 0 {
		t.Errorf("a new day adds a point: %+v", next)
	}
}

// keep stores a pool for repo as a scan would, with the oldest entry's age.
func (s *Scanner) keep(t *testing.T, repo string, oldest *int) {
	t.Helper()
	raw, _ := json.Marshal(Pool{Version: Version, Repo: repo, Measured: true, OldestDays: oldest})
	if err := s.kept().Write(keptName(repo), raw); err != nil {
		t.Fatal(err)
	}
}
