package store_test

import (
	"slices"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The exact claim and the pinned result (W-31 round 4, core block C-2;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.4–§2.8, §6 T5–T18).

func TestPinnedReviewLimit(t *testing.T) {
	t.Parallel()
	for lines, want := range map[int]time.Duration{0: time.Hour, 1: 90 * time.Minute, 300: 90 * time.Minute, 301: 2 * time.Hour,
		1200: 3 * time.Hour, 100000: 3 * time.Hour} {
		if got := store.PinnedReviewLimit(lines); got != want {
			t.Errorf("%d lines: %v, want %v", lines, got, want)
		}
	}
}

// The seat the platform registers at its start (the gap C-1 left): only
// RegisterableSeats, only when the token table names it, once.
func TestEnsureSeats(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	done, err := store.EnsureSeats(ctx, w.rt, []string{"seat:ghost"})
	ok(t, err)
	if len(done) != 0 || w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id IN ('seat:ghost', $1)`, coordinatorSeat) != 0 {
		t.Fatalf("registered %v without the coordinator's token", done)
	}
	done, err = store.EnsureSeats(ctx, w.rt, []string{"seat:ghost", coordinatorSeat})
	ok(t, err)
	if !slices.Equal(done, []string{coordinatorSeat}) {
		t.Fatalf("registered %v", done)
	}
	again, err := store.EnsureSeats(ctx, w.rt, []string{coordinatorSeat})
	ok(t, err)
	if len(again) != 0 || w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'actor_registered' AND body_json->'actors' ? $1`, coordinatorSeat) != 1 {
		t.Fatalf("a second start registered %v", again)
	}
	if w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id = 'seat:ghost'`) != 0 {
		t.Fatal("a seat outside RegisterableSeats was registered")
	}
}
