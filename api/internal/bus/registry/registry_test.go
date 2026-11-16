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
	// two with no source to publish them (RULES.md §4 item 1).
	if n := len(rocket.Channels); n != 36 {
		t.Fatalf("rocket registers %d types, want 36", n)
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
