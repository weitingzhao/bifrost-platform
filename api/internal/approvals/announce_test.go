package approvals

import (
	"context"
	"path/filepath"
	"testing"
)

// 2026-11-16: the lanes stopped after creating a card and were nudged by the
// coordinator or the duty seat; the decision never reached the bus. Every
// audited transition of a card is now handed to Announce with the card's
// number, id and catalog action.
func TestWriteAuditAnnouncesTheCard(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "approvals"), nil)
	var got []Transition
	s.Announce = func(ts []Transition) { got = append(got, ts...) }
	res := s.create(context.Background(), "s", "ensure_bifrost_namespaces", "why", "how", map[string]any{"names": []any{"bifrost-bus"}})
	if res.Approval.ID == "" || res.Approval.Number == 0 {
		t.Fatalf("create: %d %v", res.Status, res.Body)
	}
	s.writeAudit(nil, []event{{"approval.reject", res.Approval.ID, StatusRejected, "no"}})
	var last Transition
	for _, tr := range got {
		if tr.AuditAction == "approval.reject" {
			last = tr
		}
	}
	want := Transition{AuditAction: "approval.reject", ID: res.Approval.ID, Action: "ensure_bifrost_namespaces", Status: StatusRejected, Number: res.Approval.Number}
	if last != want {
		t.Fatalf("announced %+v, want %+v (all: %+v)", last, want, got)
	}
	// An audit line for something that is not a card (a sweep, a renew of a
	// gone id) is not announced.
	n := len(got)
	s.writeAudit(nil, []event{{"approval.renew", "appr_0000000000000000", "error", "gone"}})
	if len(got) != n {
		t.Fatalf("an unknown id was announced: %+v", got[n:])
	}
}
