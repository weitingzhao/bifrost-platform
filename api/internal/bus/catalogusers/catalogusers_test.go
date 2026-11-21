package catalogusers

import (
	"encoding/json"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	_ "github.com/weitingzhao/bifrost-platform/api/internal/bus/registry" // declares the body
)

// 2026-11-21, the communication line's shape for catalog.users.changed: the
// counts and a digest, never the lists; a change of who uses a package with
// the same counts still reads as a change. The control: the same users read
// back from the ledger (numbers as float64) are no change.
func TestAChangeIsTheCountsOrTheDigest(t *testing.T) {
	a := Change{Package: "api/internal/x", Prod: []string{"api/internal/a"}, Test: []string{"api/internal/b"}, Event: []string{}}
	body := Body(a)
	if body["subject"] != "api/internal/x" || body["prod"] != 1 || body["ref"] != Ref {
		t.Fatalf("%v", body)
	}
	// 2026-11-21, the communication line's review: the register compares
	// the body's ref with its own word for word; a ref with the package in
	// it would have been refused at every Append. The body must pass the
	// register's own check (positive control of the shape).
	if err := eventlog.CheckBody(Type, body); err != nil {
		t.Fatalf("the register refuses the body: %v", err)
	}
	raw, _ := json.Marshal(body)
	var kept map[string]any
	if err := json.Unmarshal(raw, &kept); err != nil {
		t.Fatal(err)
	}
	if !Same(kept, body) {
		t.Error("the same users read back must be no change")
	}
	swapped := Change{Package: a.Package, Prod: []string{"api/internal/c"}, Test: a.Test, Event: a.Event}
	if Same(kept, Body(swapped)) {
		t.Error("another user with the same counts must be a change")
	}
	if Same(nil, body) {
		t.Error("an empty stream must be a change")
	}
}
