package gate

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// 2026-11-16 (the Owner: recognise users coming and going): a declared user
// that came within its lost_after is active, one that did not is lost (also
// one never seen), and a name that came without being declared is new.
func TestRosterSeesComingAndGoing(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	declared := []registry.Client{
		{Name: "gate:stream:console", Kind: "reader", LostAfter: "24h"},
		{Name: "headrelease", Kind: "durable", LostAfter: "1h"},
		{Name: "sub-never", Kind: "durable", LostAfter: "1h"},
		{Name: "sub-quiet-row", Kind: "durable"},
	}
	polled := map[string]time.Time{"gate:stream:console": now.Add(-time.Minute), "gate:events:demo-seat": now.Add(-2 * time.Minute)}
	receipts := map[string]ReceiptCount{"headrelease": {Last: now.Add(-3 * time.Hour), Count: 4}, "gate:events:claude-8.2-doorbell": {Last: now.Add(-time.Hour), Count: 1}}
	got := map[string]RosterEntry{}
	for _, e := range Roster(declared, polled, receipts, now, now.Add(-24*time.Hour)) {
		got[e.Name] = e
	}
	want := map[string]string{
		"gate:stream:console":             RosterActive,
		"headrelease":                     RosterLost,
		"sub-never":                       RosterLost,
		"gate:events:demo-seat":           RosterNew,
		"gate:events:claude-8.2-doorbell": RosterNew,
		// 2026-11-16 drill: a dispatcher row has no lost_after; quiet is idle, not lost.
		"sub-quiet-row": RosterIdle,
	}
	for name, status := range want {
		if got[name].Status != status {
			t.Errorf("%s: %q, want %q (%+v)", name, got[name].Status, status, got[name])
		}
	}
	if got["headrelease"].Receipts != 4 || !got["headrelease"].Declared || got["gate:events:demo-seat"].Declared {
		t.Errorf("entries: %+v", got)
	}
}

// 2026-11-16, the tenant drill: after demo was removed its names stayed on
// the page as new, from receipts older than the table that dropped it. An
// undeclared name not seen since the tables were loaded is not listed; one
// seen after is new.
func TestRosterForgetsUsersThatLeft(t *testing.T) {
	now := time.Date(2026, 11, 16, 21, 45, 0, 0, time.UTC)
	loaded := now.Add(-3 * time.Minute)
	receipts := map[string]ReceiptCount{
		"sub-demo-drill":       {Last: now.Add(-5 * time.Minute), Count: 8},
		"gate:events:stranger": {Last: now.Add(-time.Minute), Count: 1},
	}
	got := Roster(nil, nil, receipts, now, loaded)
	if len(got) != 1 || got[0].Name != "gate:events:stranger" || got[0].Status != RosterNew {
		t.Fatalf("roster %+v, want only the stranger seen after the tables loaded", got)
	}
}
