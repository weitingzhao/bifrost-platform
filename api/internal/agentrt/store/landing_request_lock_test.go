package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock. TD-514: while a landing holds the stream, the pass
// must not stall on an observation it already recorded; a new status still
// waits its turn, and a disagreeing repeat is still refused.
func TestTD514RecordedRequestSkipsTheStreamLock(t *testing.T) {
	w, _, op, _, at := c4Wait(t)
	pending := c4Request(op, "pending", at)
	wrote, err := store.RecordLandingGrantRequest(ctx, w.rt, pending)
	ok(t, err)
	if !wrote {
		t.Fatal("first observation not recorded")
	}

	holder := w.db.Connect(t, w.db.Runtime)
	tx, err := holder.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.event_stream WHERE event_stream_id=$1 FOR UPDATE`, landingRepoStream)
	ok(t, err)
	before := w.lastSeq(t, landingRepoStream)

	quick, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	started := time.Now()
	wrote, err = store.RecordLandingGrantRequest(quick, w.db.Connect(t, w.db.Runtime), pending)
	if took := time.Since(started); err != nil || wrote || took > 2*time.Second {
		t.Fatalf("recorded observation queued behind the stream lock: wrote=%v err=%v after %s", wrote, err, took)
	}

	// Positive control: the lock is really held, so a new status waits for it
	// until its budget runs out, and fails for that reason only.
	short, cancelShort := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancelShort()
	approved := c4Request(op, "approved", at)
	started = time.Now()
	_, err = store.RecordLandingGrantRequest(short, w.db.Connect(t, w.db.Runtime), approved)
	if took := time.Since(started); !errors.Is(err, context.DeadlineExceeded) || took < 400*time.Millisecond {
		t.Fatalf("new observation did not wait for the stream lock: err=%v after %s", err, took)
	}
	ok(t, tx.Rollback(ctx))
	if before != w.lastSeq(t, landingRepoStream) {
		t.Fatal("audit changed while the stream was held")
	}

	wrote, err = store.RecordLandingGrantRequest(ctx, w.rt, approved)
	ok(t, err)
	if !wrote {
		t.Fatal("new observation lost once the stream was free")
	}
	bad := pending
	bad.ParamsHash = strings.Repeat("b", 64)
	if _, err = store.RecordLandingGrantRequest(ctx, w.rt, bad); err == nil || !strings.Contains(err.Error(), "identity conflict") {
		t.Fatal("disagreeing repeat not refused:", err)
	}
	if wrote, err = store.RecordLandingGrantRequest(ctx, w.rt, approved); err != nil || wrote {
		t.Fatal("repeat after the lock wrote again:", wrote, err)
	}
}
