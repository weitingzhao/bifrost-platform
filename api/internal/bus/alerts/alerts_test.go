package alerts_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store" // the seed (the controller actor), in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

var ctx = context.Background()

// 2026-11-16: the TTL cleanup job's warnings fired for 43 hours and reached
// nobody, because warnings end at the platform's webhook. Each alert the
// webhook gets is now one alert.rule.fired whose subject names its namespace,
// so the owning seat can follow match=@cicd.
func TestAnnounceWritesOneEventPerAlert(t *testing.T) {
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	a, ok := alerts.Normalize("BifrostTektonPipelineRunTTLStale", "cicd", "warning", "firing")
	b, ok2 := alerts.Normalize("NodeDown", "", "", "RESOLVED")
	if !ok || !ok2 {
		t.Fatal("normalize refused a well-formed alert")
	}
	if err := alerts.Announce(ctx, pool, []alerts.Alert{a, b}); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT body_json->>'subject', body_json->>'key', body_json->>'change' FROM agentrt.event WHERE event_type = $1 ORDER BY event_id`, alerts.Type)
	if err != nil {
		t.Fatal(err)
	}
	var got [][3]string
	for rows.Next() {
		var r [3]string
		if err := rows.Scan(&r[0], &r[1], &r[2]); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	want := [][3]string{
		{"BifrostTektonPipelineRunTTLStale@cicd", "cicd", "firing"},
		{"NodeDown@cluster", "cluster", "resolved"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// A ledger the platform has not seeded (no controller) announces nothing and
// does not fail the webhook.
func TestAnnounceWithoutControllerWritesNothing(t *testing.T) {
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	a, _ := alerts.Normalize("X", "cicd", "warning", "firing")
	if err := alerts.Announce(ctx, pool, []alerts.Alert{a}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1`, alerts.Type).Scan(&n); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestNormalizeAndBody(t *testing.T) {
	for _, bad := range [][4]string{
		{"", "cicd", "warning", "firing"},
		{"has space", "cicd", "warning", "firing"},
		{"X", "Not_A_Namespace", "warning", "firing"},
		{"X", "cicd", "warning", "pending"},
	} {
		if _, ok := alerts.Normalize(bad[0], bad[1], bad[2], bad[3]); ok {
			t.Errorf("normalize took %q", bad)
		}
	}
	a, _ := alerts.Normalize("X", "cicd", "Warning", "firing")
	if err := eventlog.CheckBody(alerts.Type, alerts.Body(a)); err != nil {
		t.Fatalf("positive control: %v", err)
	}
	bad := alerts.Body(a)
	bad["subject"] = "X"
	if eventlog.CheckBody(alerts.Type, bad) == nil {
		t.Error("a subject without the namespace is taken")
	}
}

func TestAlertsAgreeWithTheRegister(t *testing.T) {
	if err := registry.Agree(alerts.Type); err != nil {
		t.Error(err)
	}
}
