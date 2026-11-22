package coord_test

import (
	"slices"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

// The exact claim and the pinned result (W-31 round 4, core block C-2;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.4–§2.8, §6 T5–T18).

// The seat the platform registers at its start (the gap C-1 left): only
// RegisterableSeats, only when the token table names it, once.
func TestEnsureSeats(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	done, err := coord.EnsureSeats(ctx, w.rt, []string{"seat:ghost"})
	ok(t, err)
	if len(done) != 0 || w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id IN ('seat:ghost', $1)`, coordinatorSeat) != 0 {
		t.Fatalf("registered %v without the coordinator's token", done)
	}
	done, err = coord.EnsureSeats(ctx, w.rt, []string{"seat:ghost", coordinatorSeat})
	ok(t, err)
	if !slices.Equal(done, []string{coordinatorSeat}) {
		t.Fatalf("registered %v", done)
	}
	again, err := coord.EnsureSeats(ctx, w.rt, []string{coordinatorSeat})
	ok(t, err)
	if len(again) != 0 || w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'actor_registered' AND body_json->'actors' ? $1`, coordinatorSeat) != 1 {
		t.Fatalf("a second start registered %v", again)
	}
	if w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id = 'seat:ghost'`) != 0 {
		t.Fatal("a seat outside RegisterableSeats was registered")
	}
}

// The 2026-11-22 resident pilot needs independent mailboxes. Installing a
// platform image without the STG seat tokens must not register those seats.
func TestEnsure4090Seats(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	station := "seat:stg-4090-station"
	pi := "seat:stg-4090-pi"
	audit := "seat:stg-4090-audit"
	want := []string{station, pi, audit}
	done, err := coord.EnsureSeats(ctx, w.rt, []string{coordinatorSeat})
	ok(t, err)
	if !slices.Equal(done, []string{coordinatorSeat}) || w.count(t,
		`SELECT count(*) FROM agentrt.actor WHERE actor_id LIKE 'seat:stg-4090-%'`) != 0 {
		t.Fatalf("pilot seats must not register without their own tokens: %v", done)
	}
	done, err = coord.EnsureSeats(ctx, w.rt, append([]string{"seat:stg-4090-unused"}, want...))
	ok(t, err)
	if !slices.Equal(done, want) {
		t.Fatalf("registered %v, want %v", done, want)
	}
}
