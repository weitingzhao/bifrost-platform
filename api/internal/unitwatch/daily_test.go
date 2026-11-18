package unitwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
)

// fakeMain is main for the daily run: commit n's parent is n-1, and the key of
// the task "api test" changes at keyFrom.
type fakeMain struct {
	head, keyFrom int
	verdict       string
	starts        []string
	startErr      error
	now           time.Time
}

func (m *fakeMain) key(n int) string {
	if n >= m.keyFrom {
		return "new"
	}
	return "old"
}

func (m *fakeMain) daily(t *testing.T) *Daily {
	return &Daily{
		DB:   ledger(t),
		Head: func(context.Context, string) (string, error) { return sha(m.head), nil },
		FirstParent: func(_ context.Context, _, commit string) (string, error) {
			if n := (&history{}).number(commit); n > 1 {
				return sha(n - 1), nil
			}
			return "", nil
		},
		Start: func(_ context.Context, _, commit string) error {
			if m.startErr != nil {
				return m.startErr
			}
			m.starts = append(m.starts, commit)
			return nil
		},
		Tasks: func(_ context.Context, _, commit string) ([]DailyTask, error) {
			return []DailyTask{{Unit: "api", Task: "test", Key: m.key((&history{}).number(commit))}}, nil
		},
		Verdict: func(context.Context, string, DailyTask) (string, error) { return m.verdict, nil },
		Now:     func() time.Time { return m.now },
	}
}

func openDaily(t *testing.T, d *Daily) []regressions.Regression {
	t.Helper()
	open, err := regressions.List(context.Background(), d.DB, watchedRepo, true)
	if err != nil {
		t.Fatal(err)
	}
	return open
}

// 2026-11-18, the coordinator: the one-off unit run caught a real undeclared
// input and nobody would have been told. A daily run starts once, in its hour;
// a failure is recorded against the commit that gave the task its inputs, so
// the wake reaches its author, and closes when the failure is gone.
// Positive control: recording against the head instead fails the sha(4) check.
func TestDailyRunStartsOnceAndReportsTheIntroducingCommit(t *testing.T) {
	ctx := context.Background()
	chicago, _ := time.LoadLocation("America/Chicago")
	m := &fakeMain{head: 6, keyFrom: 4, verdict: evidence.EvidenceNone, now: time.Date(2026, 11, 19, 4, 59, 0, 0, chicago)}
	d := m.daily(t)
	if err := d.Pass(ctx); err != nil || len(m.starts) != 0 {
		t.Fatalf("before its hour: %v %v", err, m.starts)
	}
	m.now = m.now.Add(2 * time.Minute)
	for i := 0; i < 2; i++ {
		if err := d.Pass(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.starts) != 1 || m.starts[0] != sha(6) || len(openDaily(t, d)) != 0 {
		t.Fatalf("starts %v, open %v", m.starts, openDaily(t, d))
	}
	m.verdict = evidence.EvidenceRed
	if err := d.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	open := openDaily(t, d)
	if len(open) != 1 || open[0].Introduced != sha(4) || !strings.HasPrefix(open[0].Impact, dailyPrefix) || !strings.Contains(open[0].Impact, "api test") {
		t.Fatalf("open = %+v", open)
	}

	// The next day the same failure is not recorded twice; the day after it
	// has gone and the regression closes.
	m.now, m.head = m.now.Add(24*time.Hour), 7
	if err := d.Pass(ctx); err != nil || len(openDaily(t, d)) != 1 {
		t.Fatalf("%v %+v", err, openDaily(t, d))
	}
	m.now, m.verdict = m.now.Add(24*time.Hour), evidence.EvidenceGreen
	if err := d.Pass(ctx); err != nil || len(openDaily(t, d)) != 0 || len(m.starts) != 3 {
		t.Fatalf("%v open %+v starts %v", err, openDaily(t, d), m.starts)
	}
}

// Same origin: no node for the units (only the slow disk fits) starts nothing
// and is tried again within the hour, and nothing is reported.
// Positive control: marking the day before Start succeeds fails the last check.
func TestDailyRunRetriesAStartThatFailed(t *testing.T) {
	ctx := context.Background()
	chicago, _ := time.LoadLocation("America/Chicago")
	m := &fakeMain{head: 2, keyFrom: 1, verdict: evidence.EvidenceGreen, now: time.Date(2026, 11, 19, 5, 0, 0, 0, chicago), startErr: errors.New("no node")}
	d := m.daily(t)
	if err := d.Pass(ctx); err == nil {
		t.Fatal("a failed start was not reported")
	}
	m.startErr, m.now = nil, m.now.Add(5*time.Minute)
	if err := d.Pass(ctx); err != nil || len(m.starts) != 1 {
		t.Fatalf("%v %v", err, m.starts)
	}
}
