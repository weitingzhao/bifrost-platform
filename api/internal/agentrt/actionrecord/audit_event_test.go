package actionrecord

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Origin: 2026-11-18, S02: the audit log's lines reach the bus on the topic the
// register made for them, which until then only Agent action records used. Real
// lines have dotted actions and statuses a record does not.
func TestAnAuditLineFitsTheEvent(t *testing.T) {
	for _, c := range [][3]string{
		{"operator", "delivery.release_window.hold", "ok"},
		{"reporter", "ops-agent.alertmanager", "accepted"},
	} {
		if err := eventlog.CheckBody(TypeRecorded, AuditBody(c[0], c[1], c[2])); err != nil {
			t.Errorf("%v: %v", c, err)
		}
	}
	if err := eventlog.CheckBody(TypeRecorded, Body(7, Record{Seat: "thread:a", Action: "start_run", Outcome: OutcomeDone})); err != nil {
		t.Errorf("a record: %v", err)
	}
	bad := AuditBody("operator", "a.b", "ok")
	bad["subject"] = "other:a.b"
	if eventlog.CheckBody(TypeRecorded, bad) == nil {
		t.Error("a subject that is not who and what was taken")
	}
	if err := registry.Agree(TypeRecorded); err != nil {
		t.Error(err)
	}
}
