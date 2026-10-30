package store

import (
	"testing"
	"time"
)

func TestStgFailedRunWrongInputRequiresPersistentBlock(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	h.add(h.start(1))
	h.terminal(1, "Failed")
	for _, outcome := range []string{"verified", "superseded"} {
		h.reject(stgRecord("verify", "try", 1, "outcome", outcome, "why", "wrong_input", "evidence", "cluster/cicd/base-uid"), "conflict")
	}
	for _, why := range []string{"rollout", "health"} {
		h.reject(stgRecord("verify", "try", 1, "outcome", "failed", "why", why, "evidence", ""), "conflict")
	}
	v := h.add(stgRecord("verify", "try", 1, "outcome", "failed", "why", "wrong_input", "evidence", ""))
	h.add(stgRecord("effect", "try", 1, "class", "this_try", "workloads", map[string]any{"observed": true}, "at", h.at.Format(time.RFC3339Nano)))
	h.reject(stgRecord("retry", "try", 2, "not_before", h.at.Add(time.Hour).Format(time.RFC3339Nano)), "transition")
	h.reject(memberRecord(m, "released", v.Seq), "transition")
	block := h.block("wrong_input", m)
	h.add(memberRecord(m, "released", block.Seq))
	h.settle(m)
	open := h.opened(m)
	open["batch"] = "stg:bifrost-platform:2"
	h.reject(open, "transition")
	if !h.state().Blocks[block.Record.String("block")].IsOpen() {
		t.Fatal("settlement closed the wrong_input interval")
	}
}
