package store

import (
	"testing"
	"time"
)

func TestExternalFactsBodies(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("offset", 3600))
	a := ApprovalFact{Number: 1, Status: "executed", Action: "deploy", Tier: "T2", CreatedAt: at, DecidedAt: at, Expired: false}
	p := PipelineRun{Run: "run", UID: "uid", Pipeline: "pipeline", Kind: "delivery", Result: "Succeeded", CreatedAt: at, StartedAt: at, CompletedAt: at, Reason: "Completed"}
	q := Question{ID: "question", Status: "open", CreatedAt: at.UTC().Format(time.RFC3339), WithdrawnAt: at.UTC().Format(time.RFC3339), ExpiredAt: at.UTC().Format(time.RFC3339)}
	qb, _, err := questionSeenBody(q.ID, q)
	if err != nil {
		t.Fatal(err)
	}
	for kind, b := range map[string]map[string]any{"approval_seen": a.Body(), "question_seen": qb, "pipeline_seen": p.Body()} {
		if err := checkBody(kind, b); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		b["unknown"] = "secret"
		if checkBody(kind, b) == nil {
			t.Fatalf("%s accepted unknown field", kind)
		}
		delete(b, "unknown")
		for _, field := range []string{"created_at", "decided_at", "answered_at", "withdrawn_at", "expired_at", "started_at", "completed_at"} {
			if old, ok := b[field]; ok {
				b[field] = "not an instant"
				if checkBody(kind, b) == nil {
					t.Fatalf("%s accepted %s", kind, field)
				}
				b[field] = old
			}
		}
	}
	if got := a.Body()["created_at"]; got != "2025-12-31T23:00:00Z" {
		t.Fatalf("not UTC: %v", got)
	}
	b := p.Body()
	b["reason"] = "two words"
	if checkBody("pipeline_seen", b) == nil {
		t.Fatal("accepted multi-word reason")
	}
}
