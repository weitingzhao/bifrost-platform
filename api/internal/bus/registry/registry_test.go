package registry

import (
	"strings"
	"testing"
)

// 2026-11-16: the bus's step 0 (RULES.md §5) makes the register the one
// closed vocabulary of the event lane; the files compiled in must pass it.
func TestRegisteredTenantsPass(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	rocket, ok := all["rocket"]
	if !ok {
		t.Fatal("no rocket tenant registered")
	}
	// 36 = the 38 types proposed in the 2026-11-16 disposition table less the
	// two with no source to publish them (RULES.md §4 item 1); and 2026-11-17
	// main.regression.known and .fixed (known regressions on main): 38.
	if n := len(rocket.Channels); n != 38 {
		t.Fatalf("rocket registers %d types, want 38", n)
	}
	for _, typ := range []string{"git.main.advanced", "release.window.changed", "seat.mail.arrived"} {
		if _, ok := rocket.Channels[typ]; !ok {
			t.Errorf("rocket does not register %s", typ)
		}
	}
	if got := rocket.Channels["git.main.advanced"].NATSSubject("rocket", "stg"); got != "ev.rocket.stg.git.main.advanced" {
		t.Errorf("subject %q", got)
	}
}

const good = `asyncapi: 3.0.0
x-bifrost-tenant: rocket
channels:
  git.main.advanced:
    address: ev.rocket.{env}.git.main.advanced
    x-bifrost:
      version: 1
      lane: event
      guarantee: ordered
      key: repo
      subject: "<repo>@<sha40>"
      publisher: gate-git
      ref: "/api/v1/delivery/context?repo={repo}"
      reconcile: "gate-git compares Gitea branches/main"
      on_unread: stale
      retention: 30d
      consumers: [platform-workers]
`

// 2026-11-16: positive control for the refusals below — the same file
// with nothing broken parses.
func TestParseGood(t *testing.T) {
	if _, err := Parse("rocket.yaml", []byte(good)); err != nil {
		t.Fatal(err)
	}
}

// 2026-11-16: each rule of RULES.md §5 refuses its own breakage; the
// disposition table's items 5 (no failure direction) and 39 (reconcile
// "none" without a reason) were gaps of the first design.
func TestParseRefuses(t *testing.T) {
	cases := map[string][2]string{
		"two-segment type":     {"git.main.advanced:\n    address: ev.rocket.{env}.git.main.advanced", "git.main:\n    address: ev.rocket.{env}.git.main"},
		"address of another":   {"address: ev.rocket.{env}.git.main.advanced", "address: ev.rocket.{env}.git.main.moved"},
		"command lane":         {"lane: event", "lane: command"},
		"ordered without key":  {"      key: repo\n", ""},
		"unknown guarantee":    {"guarantee: ordered", "guarantee: maybe"},
		"no failure direction": {"on_unread: stale", "on_unread: \"\""},
		"bare none":            {"reconcile: \"gate-git compares Gitea branches/main\"", "reconcile: none"},
		"no consumers":         {"consumers: [platform-workers]", "consumers: []"},
		"bad retention":        {"retention: 30d", "retention: forever"},
		"wrong file name":      {"x-bifrost-tenant: rocket", "x-bifrost-tenant: trade"},
	}
	for name, c := range cases {
		doc := strings.Replace(good, c[0], c[1], 1)
		if doc == good {
			t.Fatalf("%s: the case changed nothing", name)
		}
		if _, err := Parse("rocket.yaml", []byte(doc)); err == nil {
			t.Errorf("%s: parsed, want a refusal", name)
		}
	}
}

// 2026-11-17, the coordinator: the register named the same three readers
// (platform-workers, platform-api, seat:*) on every topic, so "who reads this"
// could not be read off it. It names only readers the code runs: headrelease
// (its durable consumer, server/bus_wire.go headReleaseWakes) on the three
// events it consumes, the inbox waiters (bus/mailwake,
// 2026-11-18, channel W01) on seat.mail.arrived, the conclusions' consumers
// (server consumeConclusions, 2026-11-18) on what ends a card or a
// question, the window queue (server consumeWindowQueue, 2026-11-18) on the
// window's changes, and the Bus page (console) on
// every topic. Subscription
// rows are their own register; a seat's ad hoc wait shows in the receipts.
func TestRegisterNamesRealReaders(t *testing.T) {
	tenants, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	ch := tenants["rocket"].Channels
	if len(ch) == 0 {
		t.Fatal("no rocket register loaded")
	}
	headrelease := map[string]bool{"git.main.advanced": true, "ci.run.finished": true, "release.window.changed": true}
	for typ, c := range ch {
		has, console := false, false
		for _, r := range c.Consumers {
			switch r {
			case "headrelease":
				has = true
			case "console":
				console = true
			case "release-recorder":
				if typ != "ci.run.finished" {
					t.Errorf("%s: the release recorder reads only finished runs", typ)
				}
			case "alert-letters":
				if typ != "alert.rule.fired" {
					t.Errorf("%s: the alert letters read only alerts", typ)
				}
			case "window-queue":
				if typ != "release.window.changed" {
					t.Errorf("%s: the window queue reads only the window's changes", typ)
				}
			case "unit-watch":
				if typ != "git.main.advanced" {
					t.Errorf("%s: the unit declaration watch reads only main's advances", typ)
				}
			case "card-created-notice":
				if typ != "approval.card.created" {
					t.Errorf("%s: the card notice reads only a new card", typ)
				}
			case "card-conclusions":
				if typ != "approval.card.decided" && typ != "approval.card.executed" {
					t.Errorf("%s: the card conclusions read only a card's decision and result", typ)
				}
			case "question-conclusions":
				if typ != "owner.question.answered" {
					t.Errorf("%s: the question conclusions read only the answer", typ)
				}
			case "inbox-waiters":
				if typ != "seat.mail.arrived" {
					t.Errorf("%s: the inbox waiters read only seat.mail.arrived", typ)
				}
			default:
				t.Errorf("%s: reader %q is run by no code", typ, r)
			}
		}
		if !console {
			t.Errorf("%s: the Bus page reads every topic; console is missing", typ)
		}
		if has != headrelease[typ] {
			t.Errorf("%s: headrelease reads it = %v, its consumer's filter says %v", typ, has, headrelease[typ])
		}
	}
}
