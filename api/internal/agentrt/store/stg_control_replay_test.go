package store

import "testing"

// The B5-g card transport is deliberately absent here. Replay its already
// committed answer/authorization while Ack keeps failing: folding that same
// durable history cannot mint a second budget generation or erase its trigger.
func TestStgControlRecordedAnswerReplayCannotAuthorizeTwice(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgExhausted(t)
	block := h.block("budget_exhausted", m)
	grant := stgRecord("granted", "grant", "q-ack-retry", "members", []any{m}, "gen", 2, "trigger_seq", block.Seq, "question_id", "q-ack-retry")
	h.reject(grant, "permission")
	h.ownerOutcome("q-ack-retry", block, "retry_release", m)
	h.add(grant)
	before := h.state()
	for range 100 {
		s := h.state()
		if s.LastSeq != before.LastSeq || len(s.Grants) != 1 || s.generation(stgSHA("a")) != 2 || s.Blocks[block.Record.String("block")].Opened.Seq != block.Seq {
			t.Fatal("replay changed authorization or durable trigger")
		}
	}
	h.reject(grant, "duplicate")
	h.add(stgUnblock(block, "granted", "q-ack-retry"))
	if h.state().generation(stgSHA("a")) != 2 {
		t.Fatal("unblocking minted another generation")
	}
}
