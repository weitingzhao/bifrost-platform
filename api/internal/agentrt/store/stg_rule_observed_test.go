package store

import (
	"testing"
	"time"
)

// R5:5.7 B5 fix: a "rule" block opened on a transient misread lifts when the
// policy reads enabled again for the same rule version; a disabled read never
// lifts it, and a newer version still does.
func TestStgRuleBlockLiftsOnEnabledObservation(t *testing.T) {
	t.Parallel()
	ruleBlock := func(h *stgHarness) StgEvent {
		return h.add(stgRecord("blocked", "block", StgBlockID(stgSHA("b"), stgSHA("e"), "rule", "v1"), "why", "rule", "P", stgSHA("b"), "base", stgTestBase(), "first_at", h.at.Format(time.RFC3339Nano), "evidence_version", "v1", "detail", map[string]any{"rules": map[string]any{"dev": 5}}))
	}
	observed := func(h *stgHarness, block StgEvent, version int, enabled any) map[string]any {
		r := stgRecord("rule_observed", "block", block.Record.String("block"), "interval_seq", block.Seq, "rule_id", "landing:dev", "rule_version", version, "read_at", h.at.Format(time.RFC3339Nano))
		if enabled != nil {
			r["enabled"] = enabled
		}
		return r
	}
	t.Run("same_version_enabled", func(t *testing.T) {
		t.Parallel()
		h := stgReady(t, stgTestMember())
		block := ruleBlock(h)
		proof := h.add(observed(h, block, 5, true))
		h.add(stgUnblock(block, "rule_changed", proof.Seq))
		if h.state().Blocks[block.Record.String("block")].IsOpen() {
			t.Fatal("an enabled read of the blocked rule version left the block open")
		}
	})
	t.Run("disabled_never_lifts", func(t *testing.T) {
		t.Parallel()
		h := stgReady(t, stgTestMember())
		block := ruleBlock(h)
		h.reject(observed(h, block, 5, false), "transition")
		h.reject(observed(h, block, 6, false), "transition")
		// Before the fix, the same version never counted; nor does an
		// unflagged observation of it now.
		h.reject(observed(h, block, 5, nil), "transition")
		h.reject(observed(h, block, 4, true), "transition")
		h.reject(observed(h, block, 5, "yes"), "invalid")
		if !h.state().Blocks[block.Record.String("block")].IsOpen() {
			t.Fatal("block closed without an enabled observation")
		}
	})
	t.Run("newer_version", func(t *testing.T) {
		t.Parallel()
		for _, enabled := range []any{true, nil} {
			h := stgReady(t, stgTestMember())
			block := ruleBlock(h)
			proof := h.add(observed(h, block, 6, enabled))
			h.add(stgUnblock(block, "rule_changed", proof.Seq))
			if h.state().Blocks[block.Record.String("block")].IsOpen() {
				t.Fatalf("enabled=%v: a newer rule version left the block open", enabled)
			}
		}
	})
	t.Run("read_before_block", func(t *testing.T) {
		t.Parallel()
		h := stgReady(t, stgTestMember())
		block := ruleBlock(h)
		r := observed(h, block, 5, true)
		r["read_at"] = block.At.Format(time.RFC3339Nano)
		h.reject(r, "transition")
	})
}
