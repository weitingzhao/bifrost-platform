package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 443 B1: a rule's answer whose note its transaction did not keep is
// noted by the decision that uses it, which also acknowledges it, so no scan
// of unacknowledged answers is needed for it.
func TestRuleNoteWrittenByTheDecision(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.overBudget(t, "M-3", "")
	if n, err := store.BlockSpentForTest(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := s.order[0]
	// Answered by the rule in the question store; the database kept nothing.
	ok(t, s.answer(ctx, card, "c", "rule card-auto-answer v1", time.Now().Add(time.Hour), ""))
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	var notes int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND mission_id = 'M-3'
	  AND body_json->>'text' = 'rule card-auto-answer v1 answered question ' || $1 || ' with c'`, card).Scan(&notes))
	if notes != 1 || s.acked[card] != 1 || w.state(t, "mission", "M-3") != "stopped" {
		t.Fatalf("%d notes, acknowledged %d, M-3 %s", notes, s.acked[card], w.state(t, "mission", "M-3"))
	}
}
