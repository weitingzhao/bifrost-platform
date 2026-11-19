//bifrost:owner delivery

package server

import (
	"testing"
)

// 2026-11-16 night: of 564 s from main to STG released, 267 s were waiting
// for the release window; a pass that found it held waited for the next
// 5-minute reconciliation. The window's change now wakes head-release too.
func TestHeadReleaseWakesOnTheWindow(t *testing.T) {
	want := map[string]bool{"git.main.advanced": false, "ci.run.finished": false, "release.window.changed": false}
	for _, typ := range headReleaseWakes {
		want[typ] = true
	}
	for typ, ok := range want {
		if !ok {
			t.Errorf("head-release is not woken by %s", typ)
		}
	}
}

// 2026-11-17, with the communication layer: the newest event worth a
// head-release pass is the cause a started run names; a start clears it only
// if no newer wake came meanwhile. Positive control: with clearWake storing
// "" unconditionally, the last check sees "" instead of ev-3.
func TestHeadReleaseRemembersTheNewestWake(t *testing.T) {
	var b busWiring
	if b.lastWake() != "" {
		t.Fatal("no wake yet")
	}
	b.clearWake("ev-1") // nothing kept: no panic, nothing changes
	b.noteWake("ev-1")
	b.noteWake("ev-2")
	if b.lastWake() != "ev-2" {
		t.Fatalf("lastWake = %q", b.lastWake())
	}
	b.clearWake("ev-2")
	if b.lastWake() != "" {
		t.Fatalf("a run started with ev-2 left %q", b.lastWake())
	}
	b.noteWake("ev-3")
	b.clearWake("ev-2") // a start that read ev-2 before ev-3 came
	if b.lastWake() != "ev-3" {
		t.Fatalf("a newer wake was lost: %q", b.lastWake())
	}
}
