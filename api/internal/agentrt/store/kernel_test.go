package store_test

import (
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

const (
	commitA = "2333e00753f91c74c8b2f4d693a803f8e89e7892"
	commitB = "0b9e1f3c7a1d2e4f5a6b7c8d9e0f1a2b3c4d5e6f"
	noSuch  = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

// newKernelWorld is a world with mission M-1 active and hosts h1–h8.
func newKernelWorld(t *testing.T) *world {
	t.Helper()
	ctx := ctxOf(t)
	w := newWorld(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	w.seeded(t, func(tx pgx.Tx) {
		for i := 1; i <= 8; i++ {
			seedHost(t, tx, fmt.Sprintf("h%d", i))
		}
	})
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	return w
}

// waitFor returns once a backend of this database waits for a lock in a
// statement containing fragment, and fails the test if that has not happened
// by the deadline: a race the test means to stage did not happen.
func (w *world) waitFor(t *testing.T, fragment string, deadline time.Time) {
	t.Helper()
	w.waitForN(t, fragment, 1, deadline)
}

// waitForN is waitFor for at least n backends.
func (w *world) waitForN(t *testing.T, fragment string, want int, deadline time.Time) {
	t.Helper()
	ctx := ctxOf(t)
	for {
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
		  AND wait_event_type = 'Lock' AND position($1 in query) > 0`, fragment).Scan(&n))
		if n >= want {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("nothing waited for a lock in %q in time; the case proves nothing", fragment)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (w *world) artifact(t *testing.T, kind, body string) string {
	t.Helper()
	ctx := ctxOf(t)
	var id string
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = artifact.PutArtifact(ctx, tx, "controller", kind, "text/plain", []byte(body))
		return err
	}))
	return id
}

func (w *world) control(t *testing.T, scope, ref, kind string) string {
	t.Helper()
	got, err := coord.RequestControl(ctx, w.rt, wr("owner"), "k-"+eventlog.NewULID(time.Now()), scope, ref, kind, "test")
	ok(t, err)
	return got["control_id"].(string)
}

func (w *world) chainHolds(t *testing.T, stream string) {
	t.Helper()
	if _, err := store.VerifyStream(ctx, w.rt, stream); err != nil {
		t.Fatalf("%s: %v", stream, err)
	}
}

func is(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v; want %v", err, target)
	}
}

func same(t *testing.T, got, want map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answer %v; want the first answer %v", got, want)
	}
}
