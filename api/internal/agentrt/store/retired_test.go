package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Store cut 1 (LANE-BIZ-ALIGN, 2026-11-14) removed the checks that guarded
// every write to the landing, landing-card, STG-sample and wave streams, with
// the code that wrote them. Without a refusal an event on one of those
// streams, or a note carrying one of their records, would now be appended
// unchecked beside the rows the old checks vouched for. Append refuses both,
// saying the stream is retired, and writes nothing.
func TestRetiredStreamsRefuseWrites(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	count := func() (n int) {
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&n))
		return n
	}
	before := count()
	for name, ev := range map[string]store.Event{
		"landing stream":       {Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "x"}},
		"landing cards stream": {Stream: "landing-cards:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "x"}},
		"stg sample stream":    {Stream: "stg-sample:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "x"}},
		"wave stream":          {Stream: "wave:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "x"}},
		"landing record":       {Stream: "controller", Type: "note", Actor: "controller", Body: map[string]any{"text": "x", "landing": map[string]any{"kind": "claimed"}}},
		"stg release record":   {Stream: "controller", Type: "note", Actor: "controller", Body: map[string]any{"text": "x", "stg_release": map[string]any{}}},
		"wave record":          {Stream: "controller", Type: "note", Actor: "controller", Body: map[string]any{"text": "x", "wave_opened": map[string]any{}}},
	} {
		err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, ev)
			return err
		})
		if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "is retired") {
			t.Errorf("%s: %v; want an invalid request saying it is retired", name, err)
		}
	}
	if after := count(); after != before {
		t.Fatalf("refused writes left %d events", after-before)
	}
	// A note without those records on a stream that is not retired is still written.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "controller", Type: "note", Actor: "controller", Body: map[string]any{"text": "still written"}})
		return err
	}))
	if after := count(); after != before+1 {
		t.Fatalf("an ordinary note wrote %d events, want 1", after-before)
	}
}
