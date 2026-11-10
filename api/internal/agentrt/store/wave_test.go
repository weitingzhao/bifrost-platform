package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func waveForms() map[string]map[string]any {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	return map[string]map[string]any{
		"wave_opened":         {"v": "1", "wave": "w", "repo": "bifrost-platform", "ref": "refs/heads/wave/R6/1", "plan_root": "R6", "n": 1, "opened_by": "owner", "base": a},
		"wave_appended":       {"v": "1", "wave": "w", "task": "t1", "commit": b, "parent": a, "change_id": "change1", "rebased": false},
		"wave_append_refused": {"v": "1", "wave": "w", "task": "t2", "why": "excluded", "paths": []any{"api/x.go"}},
		"wave_closed":         {"v": "1", "wave": "w", "by": "controller"},
		"wave_landing":        {"v": "1", "wave": "w", "batch": "batch1", "M0": a, "R": b},
		"wave_red":            {"v": "1", "wave": "w", "batch": "batch1", "blamed": []any{"t1"}, "prefix": []any{}},
		"wave_landed":         {"v": "1", "wave": "w", "R": b, "main": b},
		"wave_abandoned":      {"v": "1", "wave": "w", "by": "owner", "why": "cancelled"},
		"wave_tally":          {"v": "1", "wave": "w", "landing_ci": 1, "stg": 2, "prod": 0},
	}
}

func putWave(w *world, name string, body map[string]any) error {
	return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "wave:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "wave", name: body}})
		return err
	})
}

// TestDB coverage exercises validation through Append, including its stream lock.
func TestWaveForms(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for name, body := range waveForms() {
		t.Run(name, func(t *testing.T) {
			for key := range body {
				bad := map[string]any{}
				for k, v := range body {
					bad[k] = v
				}
				delete(bad, key)
				if err := putWave(w, name, bad); err == nil || errors.Is(err, store.ErrWaveRefused) {
					t.Fatalf("missing %s: form was not rejected: %v", key, err)
				}
				bad[key] = map[string]any{"invalid": true}
				if err := putWave(w, name, bad); err == nil || errors.Is(err, store.ErrWaveRefused) {
					t.Fatalf("bad %s: form was not rejected: %v", key, err)
				}
			}
			bad := map[string]any{}
			for k, v := range body {
				bad[k] = v
			}
			bad["extra"] = true
			if err := putWave(w, name, bad); err == nil || errors.Is(err, store.ErrWaveRefused) {
				t.Fatalf("extra field: %v", err)
			}
		})
	}
}

func TestWaveFoldAndTransitions(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	forms := waveForms()
	put := func(name string) { t.Helper(); ok(t, putWave(w, name, forms[name])) }
	refused := func(name string) {
		t.Helper()
		if err := putWave(w, name, forms[name]); !errors.Is(err, store.ErrWaveRefused) {
			t.Fatalf("%s: expected refusal, got %v", name, err)
		}
	}
	state := func() store.WaveState {
		t.Helper()
		var s map[string]store.WaveState
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			s, err = store.WaveStateOf(ctx, tx, "bifrost-platform")
			return err
		}))
		return s["w"]
	}
	for name := range forms {
		if name != "wave_opened" {
			refused(name)
		}
	}
	put("wave_opened")
	if s := state(); s.State != "open" || s.Head != strings.Repeat("a", 40) {
		t.Fatal(s)
	}
	refused("wave_opened")
	forms["wave_opened"]["wave"] = "other"
	refused("wave_opened")
	forms["wave_opened"]["wave"] = "w"
	forms["wave_opened"]["wave"] = "wrong-repo"
	forms["wave_opened"]["repo"] = "other"
	refused("wave_opened")
	forms["wave_opened"]["repo"] = "bifrost-platform"
	forms["wave_opened"]["wave"] = "w"
	refused("wave_landing")
	refused("wave_red")
	refused("wave_landed")
	put("wave_append_refused")
	put("wave_appended")
	forms["wave_appended"]["parent"] = strings.Repeat("b", 40)
	forms["wave_appended"]["commit"] = strings.Repeat("c", 40)
	forms["wave_appended"]["task"] = "t2"
	forms["wave_appended"]["change_id"] = "change2"
	forms["wave_appended"]["rebased"] = true
	put("wave_appended")
	s := state()
	if s.Head != strings.Repeat("c", 40) || len(s.Members) != 2 || s.Members[0].Task != "t1" || s.Members[1].Task != "t2" || !s.Members[1].Rebased || s.Members[1].ChangeID != "change2" {
		t.Fatal(s)
	}
	refused("wave_appended") // parent no longer matches the head
	put("wave_tally")
	s = state()
	if s.Tallies.LandingCI != 1 || s.Tallies.Stg != 2 || s.Tallies.Prod != 0 {
		t.Fatal(s)
	}
	put("wave_closed")
	refused("wave_appended")
	refused("wave_append_refused")
	if state().State != "closing" {
		t.Fatal(state())
	}
	refused("wave_closed")
	for _, phase := range []string{"landing", "red", "landed"} {
		put("wave_landing")
		if state().State != "landing" {
			t.Fatal(state())
		}
		refused("wave_landing")
		forms["wave_landed"]["R"] = strings.Repeat("c", 40)
		refused("wave_landed")
		forms["wave_landed"]["R"] = strings.Repeat("b", 40)
		forms["wave_red"]["batch"] = "other"
		refused("wave_red")
		forms["wave_red"]["batch"] = "batch1"
		refused("wave_appended")
		refused("wave_closed")
		if phase == "landed" {
			put("wave_landed")
			break
		}
		put("wave_red")
		if state().State != "red" {
			t.Fatal(state())
		}
		refused("wave_red")
		refused("wave_landed")
		refused("wave_appended")
	}
	if state().State != "landed" {
		t.Fatal(state())
	}
	refused("wave_landed")
	refused("wave_abandoned")
	// A distinct ref can be abandoned, and then admits no further members.
	for _, name := range []string{"wave_opened", "wave_abandoned", "wave_appended"} {
		forms[name]["wave"] = "abandon"
	}
	forms["wave_opened"]["n"] = 2
	forms["wave_opened"]["ref"] = "refs/heads/wave/R6/2"
	put("wave_opened")
	put("wave_abandoned")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		states, err := store.WaveStateOf(ctx, tx, "bifrost-platform")
		if err != nil {
			return err
		}
		if states["abandon"].State != "abandoned" {
			t.Fatal(states)
		}
		empty, err := store.WaveStateOf(ctx, tx, "other")
		if len(empty) != 0 {
			t.Fatal(empty)
		}
		return err
	}))
	refused("wave_appended")
	refused("wave_abandoned")
}

func TestWaveEnvelope(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for name, body := range waveForms() {
		for _, tc := range []struct {
			actor, stream string
			extra         bool
		}{
			{"owner", "wave:bifrost-platform", false},
			{"controller", "landing:bifrost-platform", false},
			{"controller", "wave:", false},
			{"controller", "wave:bifrost-platform", true},
		} {
			err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				b := map[string]any{"text": "wave", name: body}
				if tc.extra {
					b["wave_closed"] = waveForms()["wave_closed"]
					if name == "wave_closed" {
						b["wave_tally"] = waveForms()["wave_tally"]
					}
				}
				_, err := store.Append(ctx, tx, store.Event{Stream: tc.stream, Actor: tc.actor, Type: "note", Body: b})
				return err
			})
			if !errors.Is(err, store.ErrWaveRefused) {
				t.Fatalf("%s %+v: %v", name, tc, err)
			}
		}
	}
}
