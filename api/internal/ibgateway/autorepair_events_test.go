package ibgateway

import (
	"context"
	"errors"
	"testing"
)

// Origin: 2026-11-18, the list of problem channels (L12): the result of an
// automatic repair was only in the audit log.
func TestAutoRepairResultIsSaid(t *testing.T) {
	var ok bool
	var message string
	s := &Service{onAutoRepair: func(_ context.Context, o bool, m string) { ok, message = o, m }}
	s.sayAutoRepair(t.Context(), false, repairMessage("ignored", errors.New("redis down")))
	if ok || message != "redis down" {
		t.Fatalf("ok %v, message %q", ok, message)
	}
	(&Service{}).sayAutoRepair(t.Context(), true, "no listener is fine")
}
