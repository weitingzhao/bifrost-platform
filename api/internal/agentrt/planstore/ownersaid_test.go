package planstore_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Origin: 2026-11-25, the Owner chose automatic collection of what he says
// (「我觉得B好，需要自动收集」): the hook may post a message more than once, so
// each must be recorded once by its message_id, and read back in the order said.
func TestOwnerSaidRecordedOnce(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	tr := "7274b722-9792-4ba2-bd5c-2dd8aff76cb5"
	at := time.Date(2026, 11, 25, 13, 0, 0, 0, time.UTC)
	msgs := []planstore.OwnerSaid{
		{MessageID: "11111111-1111-4111-8111-111111111111", Transcript: tr, Thread: "local_6ab1ad67-09a8-4522-9b4a-a03125b10b2c", Host: "macpro", SaidAt: at.Add(time.Minute), Text: "我觉得B好，需要自动收集"},
		{MessageID: "22222222-2222-4222-8222-222222222222", Transcript: tr, SaidAt: at, Text: strings.Repeat("长", planstore.OwnerSaidMax)},
	}
	added, err := planstore.RecordOwnerSaid(ctx, w.rt, "controller", msgs)
	ok(t, err)
	if added != 2 {
		t.Fatalf("added %d, want 2", added)
	}
	// Positive control of the case above: posted again, nothing is added.
	again, err := planstore.RecordOwnerSaid(ctx, w.rt, "controller", msgs)
	ok(t, err)
	if again != 0 {
		t.Fatalf("posted again: added %d", again)
	}
	got, err := planstore.OwnerSaidSince(ctx, w.rt, at.Add(-time.Hour), 0)
	ok(t, err)
	if len(got) != 2 || got[0].MessageID != msgs[1].MessageID || !got[0].Truncated || len(got[0].Text) > planstore.OwnerSaidMax ||
		got[1].Text != "我觉得B好，需要自动收集" || got[1].Thread == "" || got[1].Host != "macpro" {
		t.Fatalf("read back %+v", got)
	}
	if later, _ := planstore.OwnerSaidSince(ctx, w.rt, at.Add(30*time.Second), 0); len(later) != 1 {
		t.Fatalf("since filter: %d", len(later))
	}
	for name, bad := range map[string]planstore.OwnerSaid{
		"no id":      {Transcript: tr, SaidAt: at, Text: "x"},
		"no text":    {MessageID: msgs[0].MessageID, Transcript: tr, SaidAt: at},
		"no time":    {MessageID: msgs[0].MessageID, Transcript: tr, Text: "x"},
		"bad thread": {MessageID: msgs[0].MessageID, Transcript: tr, SaidAt: at, Text: "x", Thread: "thread-7"},
	} {
		if _, err := planstore.RecordOwnerSaid(ctx, w.rt, "controller", []planstore.OwnerSaid{bad}); !errors.Is(err, eventlog.ErrInvalid) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}
