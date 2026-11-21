package cards_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor" // the seed, in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/cards"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

var ctx = context.Background()

// 2026-11-16: a seat waiting on card #n follows match=#n and is woken by the
// decision; the ledger takes one event per transition.
func TestAnnounceWritesTheCardTransitions(t *testing.T) {
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	id := "appr_0123456789abcdef"
	list := []cards.Card{
		{Type: cards.TypeOf("approval.create"), ID: id, Action: "ensure_bifrost_namespaces", Status: "pending", Number: 586},
		{Type: cards.TypeOf("approval.approve"), ID: id, Action: "ensure_bifrost_namespaces", Status: "approved", Number: 586},
		{Type: cards.TypeOf("approval.execute"), ID: id, Action: "ensure_bifrost_namespaces", Status: "executed", Number: 586},
	}
	for _, c := range list {
		if !cards.Valid(c) {
			t.Fatalf("not valid: %+v", c)
		}
	}
	if err := cards.Announce(ctx, pool, list); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT event_type, body_json->>'subject', body_json->>'change' FROM agentrt.event WHERE event_stream_id = $1 ORDER BY event_id`, cards.Stream(id))
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
		{cards.TypeCreated, "#586", "pending"},
		{cards.TypeDecided, "#586", "approved"},
		{cards.TypeExecuted, "#586", "executed"},
	}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestTypeOfAndBody(t *testing.T) {
	for a, want := range map[string]string{
		"approval.reject": cards.TypeDecided, "approval.expire": cards.TypeDecided, "approval.unknown": cards.TypeExecuted,
		"approval.queue": "", "approval.claim": "", "approval.notify": cards.TypeNotified, // a push, since 2026-11-20
	} {
		if got := cards.TypeOf(a); got != want {
			t.Errorf("TypeOf(%s) = %q, want %q", a, got, want)
		}
	}
	c := cards.Card{Type: cards.TypeDecided, ID: "appr_0123456789abcdef", Action: "x", Status: "rejected", Number: 7}
	if err := eventlog.CheckBody(c.Type, cards.Body(c)); err != nil {
		t.Fatalf("positive control: %v", err)
	}
	bad := cards.Body(c)
	bad["subject"] = "#8"
	if eventlog.CheckBody(c.Type, bad) == nil {
		t.Error("a subject not made of the number is taken")
	}
	if cards.Valid(cards.Card{Type: cards.TypeDecided, ID: "appr_0123456789abcdef", Action: "x", Status: "approved"}) {
		t.Error("a card without a number is valid")
	}
}

// The bodies carry what the register promises (its key "card" for decided).
func TestCardsAgreeWithTheRegister(t *testing.T) {
	for _, typ := range []string{cards.TypeCreated, cards.TypeDecided, cards.TypeExecuted} {
		if err := registry.Agree(typ); err != nil {
			t.Error(err)
		}
	}
}

// 2026-11-20, the metrics line: the Owner's interruptions by card pushes
// could not be counted past the last 500 audit records. A push is an
// approval.notify audit event; it reaches the ledger as approval.card.notified
// with its kind and channel.
func TestNotifyBecomesNotifiedWithKindAndChannel(t *testing.T) {
	if cards.TypeOf("approval.notify") != cards.TypeNotified {
		t.Fatalf("approval.notify maps to %q", cards.TypeOf("approval.notify"))
	}
	k, ch := cards.ParseNotice("kind=reminder_waiting channel=ntfy target=relay")
	if k != "reminder_waiting" || ch != "ntfy" {
		t.Fatalf("parsed %q %q", k, ch)
	}
	if k, ch := cards.ParseNotice("store deliveries: disk full"); k != "" || ch != "" {
		t.Fatalf("a failure without a push names none, got %q %q", k, ch)
	}
	c := cards.Card{Type: cards.TypeNotified, ID: "appr_f5d45a50b38672f8", Action: "apply_manifest", Status: "accepted", Number: 660, Kind: k, Channel: ch}
	c.Kind, c.Channel = "created", "ntfy"
	if !cards.Valid(c) {
		t.Fatal("a push card is valid")
	}
	b := cards.Body(c)
	if b["kind"] != "created" || b["channel"] != "ntfy" || b["subject"] != "#660" {
		t.Fatalf("body %v", b)
	}
	if err := eventlog.CheckBody(cards.TypeNotified, b); err != nil {
		t.Fatalf("the declared body refuses a push: %v", err)
	}
	if _, ok := cards.Body(cards.Card{Type: cards.TypeCreated, ID: c.ID, Action: c.Action, Status: "pending", Number: 660, Kind: "created"})["kind"]; ok {
		t.Fatal("only a push carries kind")
	}
}
