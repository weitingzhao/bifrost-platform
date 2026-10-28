package landing

import (
	"testing"
	"time"
)

// CUT §11.8 v3 item 1: the live mode is recorded once; operations born
// after it are live, before it not; the fence is the later of enabled and
// the mode, enabled alone without a mode.
func TestLiveModeAndFence(t *testing.T) {
	s := State{Operations: map[string]Operation{}, Reservations: map[string]string{}}
	enabled := epoch
	apply := func(at time.Time, r map[string]any) error {
		t.Helper()
		rec, err := Parse(r)
		if err != nil {
			return err
		}
		return s.Apply(Entry{At: at, Record: rec})
	}
	if err := apply(enabled, map[string]any{"v": Version, "kind": "enabled", "op": "enabled:dev", "repo": "bifrost-platform", "rule_version": 1}); err != nil {
		t.Fatal(err)
	}
	if !s.Fence().Equal(enabled) {
		t.Fatal("fence without a mode", s.Fence())
	}
	if err := apply(enabled.Add(time.Minute), map[string]any{"v": Version, "kind": "claimed", "op": "land:" + task1 + ":1", "task": task1, "attempt": attempt1, "A": A, "rule_version": 1}); err != nil {
		t.Fatal(err)
	}
	mode := enabled.Add(time.Hour)
	if err := apply(mode, map[string]any{"v": Version, "kind": "mode", "op": "mode:live", "repo": "bifrost-platform"}); err != nil {
		t.Fatal(err)
	}
	if err := apply(mode.Add(time.Second), map[string]any{"v": Version, "kind": "mode", "op": "mode:live", "repo": "bifrost-platform"}); err == nil {
		t.Fatal("a second mode")
	}
	if !s.Fence().Equal(mode) || !s.LiveAt.Equal(mode) {
		t.Fatal("fence after the mode", s.Fence())
	}
	if s.Operations["land:"+task1+":1"].Live {
		t.Fatal("an operation born before the mode is live")
	}
	if err := apply(mode.Add(2*time.Minute), map[string]any{"v": Version, "kind": "claimed", "op": "land:t_01ARZ3NDEKTSV4RRFFQ69G5FAW:1", "task": "t_01ARZ3NDEKTSV4RRFFQ69G5FAW", "attempt": attempt1, "A": A, "rule_version": 1}); err != nil {
		t.Fatal(err)
	}
	if !s.Operations["land:t_01ARZ3NDEKTSV4RRFFQ69G5FAW:1"].Live {
		t.Fatal("an operation born after the mode is not live")
	}
	// A v1 mode does not apply.
	s2 := State{Operations: map[string]Operation{}, Reservations: map[string]string{}}
	if rec, err := Parse(map[string]any{"v": "1", "kind": "mode", "op": "mode:live", "repo": "bifrost-platform"}); err == nil && s2.Apply(Entry{At: mode, Record: rec}) == nil {
		t.Fatal("a v1 mode applied")
	}
}
