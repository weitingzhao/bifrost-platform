package alerts_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor" // the seed (the controller actor), in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
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
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
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

// 2026-11-18, the Owner's "first make problems visible": the Watchdog going
// missing was known only to the relay on the Mac mini. The mirror says each
// change once, and a blip of the relay's own plane says nothing.
func TestRelayWatchSaysEachChangeOnce(t *testing.T) {
	var w alerts.RelayWatch
	ok := alerts.RelayReading{Reached: true, Enabled: true, HeartbeatOK: true}
	names := func(l []alerts.Alert) (out []string) {
		for _, a := range l {
			out = append(out, a.Name+":"+a.Status)
		}
		return out
	}
	steps := []struct {
		r    alerts.RelayReading
		want string
	}{
		{ok, ""},
		{alerts.RelayReading{}, ""}, {alerts.RelayReading{}, ""},
		{ok, ""},
		{alerts.RelayReading{}, ""}, {alerts.RelayReading{}, ""}, {alerts.RelayReading{}, alerts.NameRelayUnreachable + ":firing"}, {alerts.RelayReading{}, ""},
		{alerts.RelayReading{Reached: true, Enabled: true}, alerts.NameRelayUnreachable + ":resolved," + alerts.NameHeartbeatMissing + ":firing"},
		{alerts.RelayReading{Reached: true, Enabled: true}, ""},
		{ok, alerts.NameHeartbeatMissing + ":resolved"},
		{alerts.RelayReading{Reached: true}, ""},
	}
	for i, s := range steps {
		if got := strings.Join(names(w.Step(s.r)), ","); got != s.want {
			t.Fatalf("step %d: got %q, want %q", i, got, s.want)
		}
	}
}

// Same origin: a problem recorded in the ledger stayed under a type the bus does
// not carry. Fire says it as an alert in the same transaction, naming the record.
func TestFireNamesTheRecordItTellsOf(t *testing.T) {
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	var cause int64
	if err := eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := alerts.Fire(ctx, tx, alerts.ArtifactRefused(), 0); err != nil {
			return err
		}
		r, _, err := eventlog.Last(ctx, tx, alerts.Stream(alerts.NameArtifactRefused))
		cause = r.ID
		if err != nil {
			return err
		}
		return alerts.Fire(ctx, tx, alerts.LoopFailing(true), cause)
	}); err != nil {
		t.Fatal(err)
	}
	var got int64
	var change, severity string
	if err := pool.QueryRow(ctx, `SELECT cause_event_id, body_json->>'change', body_json->>'severity'
	  FROM agentrt.event WHERE event_stream_id = $1`, alerts.Stream(alerts.NameLoopFailing)).Scan(&got, &change, &severity); err != nil {
		t.Fatal(err)
	}
	if got != cause || change != "firing" || severity != "critical" {
		t.Fatalf("cause %d (want %d), change %q, severity %q", got, cause, change, severity)
	}
	// The Bus page proves one channel of a shared topic by a field of its events.
	if err := eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		r, ok, err := eventlog.LatestWithField(ctx, tx, alerts.Type, "severity", "critical", time.Now().Add(-time.Hour))
		if err != nil || !ok || r.Stream != alerts.Stream(alerts.NameLoopFailing) {
			t.Errorf("latest critical: %+v, %v, %v", r, ok, err)
		}
		_, ok, err = eventlog.LatestWithField(ctx, tx, alerts.Type, "severity", "none", time.Now().Add(-time.Hour))
		if err != nil || ok {
			t.Errorf("a value no event has was found: %v, %v", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// 2026-11-18, the Owner's "first make problems visible": X-A24
// (BifrostAlertmanagerWebhookFailing) had no bus event. The relay watch now says
// Prometheus's reading of it as an alert, and a Prometheus that does not answer
// changes nothing.
func TestRelayWatchSaysTheWebhookOnlyWhenPrometheusAnswers(t *testing.T) {
	var w alerts.RelayWatch
	up := alerts.RelayReading{Reached: true, Enabled: true, HeartbeatOK: true}
	if got := w.Step(up); len(got) != 0 {
		t.Fatalf("nothing known of the webhook: %v", got)
	}
	failing := up
	failing.WebhookKnown, failing.WebhookFailing = true, true
	got := w.Step(failing)
	if len(got) != 1 || got[0].Name != alerts.NameWebhookFailing || got[0].Status != "firing" || got[0].Namespace != "monitoring" {
		t.Fatalf("firing: %v", got)
	}
	if got := w.Step(failing); len(got) != 0 {
		t.Fatalf("a repeat is not said again: %v", got)
	}
	if got := w.Step(up); len(got) != 0 || !w.Webhook {
		t.Fatalf("Prometheus not answering must not change state: %v, failing=%v", got, w.Webhook)
	}
	if got := w.Step(alerts.RelayReading{WebhookKnown: true, WebhookFailing: true}); len(got) != 0 {
		t.Fatalf("an unreachable relay says nothing of the webhook it already said: %v", got)
	}
	ok := up
	ok.WebhookKnown = true
	got = w.Step(ok)
	if len(got) != 1 || got[0].Name != alerts.NameWebhookFailing || got[0].Status != "resolved" {
		t.Fatalf("resolved: %v", got)
	}
}

// 2026-11-19: a PrometheusRule lost the label Prometheus selects on and its 71
// rules were silent for 4.5 days; nothing said so. The watch says when fewer
// rule groups are loaded than in the last day, and only when Prometheus answers.
func TestRelayWatchSaysMissingRuleGroups(t *testing.T) {
	var w alerts.RelayWatch
	up := alerts.RelayReading{Reached: true, Enabled: true, HeartbeatOK: true}
	missing := up
	missing.RulesKnown, missing.RulesMissing = true, true
	if got := w.Step(missing); len(got) != 1 || got[0].Name != alerts.NameRuleGroupsMissing || got[0].Status != "firing" {
		t.Fatalf("missing rules: %v", got)
	}
	if got := w.Step(up); len(got) != 0 || !w.Rules {
		t.Fatalf("Prometheus not answering must not change state: %v", got)
	}
	back := up
	back.RulesKnown = true
	if got := w.Step(back); len(got) != 1 || got[0].Status != "resolved" {
		t.Fatalf("rules back: %v", got)
	}
}
