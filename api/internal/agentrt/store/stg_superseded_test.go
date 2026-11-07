package store

import (
	"testing"
	"time"
)

// 2026-11-06 B5 fix: a ci_* block on P lifts as superseded once a newer main
// head is recorded after it opened (main is linear, so that head contains
// P), or a Succeeded STG release of P or of such a head is recorded. The
// same P read again proves nothing, and other causes keep their contracts.
func TestStgBlockSupersededByNewerHead(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	block := h.block("ci_results", m)
	h.effective(m, stgSHA("b"), "effective")
	h.reject(stgUnblock(block, "superseded", int64(len(h.entries))), "transition")
	h.reject(stgUnblock(block, "superseded", "cluster/cicd/base-uid"), "transition") // recorded before the block
	h.effective(m, stgSHA("f"), "effective")
	h.add(stgUnblock(block, "superseded", int64(len(h.entries))))
	if b := h.state().Blocks[block.Record.String("block")]; b.IsOpen() || b.Closed.Record.String("why") != "superseded" {
		t.Fatal("block", b.Closed.Record)
	}

	// Causes outside ci_* do not lift as superseded.
	w := stgReady(t, m)
	wrong := w.block("wrong_input", m)
	w.effective(m, stgSHA("f"), "effective")
	w.reject(stgUnblock(wrong, "superseded", int64(len(w.entries))), "transition")
}

func TestStgBlockSupersededByRecordedRelease(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	for name, tc := range map[string]struct {
		p    string
		head bool
		ok   bool
	}{
		"release of P":                 {stgSHA("b"), false, true},
		"release of a newer head":      {stgSHA("f"), true, true},
		"release of an unknown commit": {stgSHA("f"), false, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := stgReady(t, m)
			block := h.block("ci_red", m)
			if tc.head {
				h.effective(m, tc.p, "effective")
			}
			ev := stgTestEvidence(h.at, "cluster/cicd/manual-2", "external", "manual-2")
			ev["P"] = tc.p
			h.add(ev)
			r := stgUnblock(block, "superseded", "cluster/cicd/manual-2")
			if tc.ok {
				h.add(r)
			} else {
				h.reject(r, "transition")
			}
		})
	}
}

// The CI of P that still cannot be read StgCIUnreadableAfter after the block
// opened lifts it with ci_unreadable; the error is kept in the record.
func TestStgBlockCIUnreadableAfterOneInterval(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	block := h.block("ci_results", m)
	unread := func() map[string]any {
		return stgRecord("ci_unreadable", "block", block.Record.String("block"), "interval_seq", block.Seq, "P", stgSHA("b"), "error", "ci_results: release CI is not accepted evidence", "read_at", h.at.Format(time.RFC3339Nano))
	}
	h.reject(unread(), "transition")
	h.at = block.At.Add(StgCIUnreadableAfter)
	proof := h.add(unread())
	h.add(stgUnblock(block, "ci_unreadable", proof.Seq))
	s := h.state()
	if b := s.Blocks[block.Record.String("block")]; b.IsOpen() || s.Records[proof.Seq].Record.String("error") == "" {
		t.Fatal("block", b.Closed.Record)
	}
	// Not for a block outside ci_*.
	w := stgReady(t, m)
	wrong := w.block("wrong_input", m)
	w.at = wrong.At.Add(StgCIUnreadableAfter)
	w.reject(stgRecord("ci_unreadable", "block", wrong.Record.String("block"), "interval_seq", wrong.Seq, "P", stgSHA("b"), "error", "x", "read_at", w.at.Format(time.RFC3339Nano)), "transition")
}

// A landed operation whose STG phase is blocked holds no landing slot: a
// second root's claim sees nothing in flight.
func TestStgBlockedLandedHoldsNoSlotLimit(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	h.landed(m["op"].(string), stgTestRoot, stgSHA("a"))
	h.block("ci_results", m)
	s := h.state()
	if st := stgRootStatus(s, stgMember(m)); st.Phase != "blocked" {
		t.Fatal("phase", st.Phase)
	}
	if n := s.Legacy.InFlight("land:two:1"); n != 0 {
		t.Fatal("a landed operation holds the slot", n)
	}
	h.raw(map[string]any{"text": "second claim", "landing": stgLegacyRecord("land:two:1", "claimed", "task", stgTestRoot2, "attempt", "a_01ARZ3NDEKTSV4RRFFQ69G5FAW", "A", stgSHA("2"), "rule_version", 1)})
	if n := h.state().Legacy.InFlight(m["op"].(string)); n != 1 {
		t.Fatal("a claimed operation holds no slot", n)
	}
}
