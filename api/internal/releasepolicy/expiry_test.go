package releasepolicy_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

type pushes struct{ titles, messages []string }

func (p *pushes) notify(_ context.Context, title, message string) error {
	p.titles = append(p.titles, title)
	p.messages = append(p.messages, message)
	return nil
}

func reminderCount(t *testing.T, window string) string {
	t.Helper()
	var b strings.Builder
	releasepolicy.WriteMetrics(&b)
	prefix := `bifrost_release_policy_reminders_sent_total{window="` + window + `"} `
	for _, line := range strings.Split(b.String(), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("no %s series", window)
	return ""
}

func TestRemindersOncePerWindow(t *testing.T) {
	f := newFixture(t)
	expires := now.Add(-time.Hour).Add(7 * 24 * time.Hour)
	p := &pushes{}
	pending := 0
	c := releasepolicy.NewChecker(f.eng, func() int { return pending }, p.notify, filepath.Join(t.TempDir(), "reminders"))
	before24 := reminderCount(t, "24h")
	tick := func(left time.Duration) {
		f.clock = expires.Add(-left)
		c.Tick(context.Background())
	}

	tick(72 * time.Hour)
	if len(p.titles) != 0 {
		t.Fatalf("pushed with 72h left: %q", p.titles)
	}
	tick(47 * time.Hour)
	tick(46 * time.Hour)
	tick(23 * time.Hour)
	tick(22 * time.Hour)
	tick(90 * time.Minute)
	tick(30 * time.Minute)
	if len(p.titles) != 3 ||
		!strings.Contains(p.titles[0], "expires in 47h") ||
		!strings.Contains(p.titles[1], "expires in 23h") ||
		!strings.Contains(p.titles[2], "expires in 90m") {
		t.Fatalf("pushes = %q", p.titles)
	}
	for _, m := range p.messages {
		if !strings.Contains(m, releasepolicy.SignCommand) || !strings.Contains(m, "rp-20261008-1100") {
			t.Fatalf("message without the sign command or policy id: %q", m)
		}
	}
	if got := reminderCount(t, "24h"); got == before24 {
		t.Fatalf("24h counter did not move (%s)", got)
	}

	// Expired with nothing waiting: silent.
	tick(-time.Hour)
	if len(p.titles) != 3 {
		t.Fatalf("pushed after expiry with nothing waiting: %q", p.titles)
	}
	// Expired with two releases waiting: one push, not one per hour.
	pending = 2
	tick(-2 * time.Hour)
	tick(-3 * time.Hour)
	if len(p.titles) != 4 || p.titles[3] != "Release policy expired, 2 releases waiting" ||
		!strings.Contains(p.messages[3], releasepolicy.SignCommand) {
		t.Fatalf("expired pushes = %q / %q", p.titles, p.messages)
	}
}

func TestLateStartSkipsEarlierWindows(t *testing.T) {
	f := newFixture(t)
	expires := now.Add(-time.Hour).Add(7 * 24 * time.Hour)
	p := &pushes{}
	c := releasepolicy.NewChecker(f.eng, nil, p.notify, "")
	// First check happens with 20h left: only the 24h reminder, and the 48h
	// one never follows.
	for _, left := range []time.Duration{20 * time.Hour, 19 * time.Hour} {
		f.clock = expires.Add(-left)
		c.Tick(context.Background())
	}
	if len(p.titles) != 1 || !strings.Contains(p.titles[0], "expires in 20h") {
		t.Fatalf("pushes = %q", p.titles)
	}
}

func TestRemindersSurviveRestart(t *testing.T) {
	f := newFixture(t)
	expires := now.Add(-time.Hour).Add(7 * 24 * time.Hour)
	path := filepath.Join(t.TempDir(), "reminders")
	p := &pushes{}
	f.clock = expires.Add(-30 * time.Hour)
	releasepolicy.NewChecker(f.eng, nil, p.notify, path).Tick(context.Background())
	releasepolicy.NewChecker(f.eng, nil, p.notify, path).Tick(context.Background())
	if len(p.titles) != 1 {
		t.Fatalf("pushes across restart = %q", p.titles)
	}
}

func TestNoPolicyWithWaitingReleasesPushes(t *testing.T) {
	f := newFixture(t)
	f.cms.Set(releasepolicy.PolicyConfigMap, nil)
	p := &pushes{}
	c := releasepolicy.NewChecker(f.eng, func() int { return 1 }, p.notify, "")
	c.Tick(context.Background())
	c.Tick(context.Background())
	if len(p.titles) != 1 || p.titles[0] != "No valid release policy, 1 releases waiting" {
		t.Fatalf("pushes = %q", p.titles)
	}
}
