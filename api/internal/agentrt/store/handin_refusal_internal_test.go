package store

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestHandinRefusalNote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err    error
		reason string
	}{
		{ErrPinNotPermitted, "pin_not_permitted"},
		{fmt.Errorf("executor rejected: %w", ErrPinNotPermitted), "pin_not_permitted"},
		{ErrMixedSource, "mixed_source_not_supported"},
		{ErrCoreMismatch, "core_mismatch"},
		{ErrState, ""},
	} {
		reason := HandinRefusalReason(tc.err)
		if reason != tc.reason {
			t.Fatalf("%v: %s", tc.err, reason)
		}
		if reason == "" {
			continue
		}
		ev := handinRefusalEvent("a", "", reason)
		if err := checkBody(ev.Type, ev.Body); err != nil {
			t.Fatal(err)
		}
		if ev.Stream != "handin-refusals" || ev.Type != "note" || len(ev.Body) != 1 {
			t.Fatalf("event: %+v", ev)
		}
		var text map[string]string
		if err := json.Unmarshal([]byte(ev.Body["text"].(string)), &text); err != nil {
			t.Fatal(err)
		}
		if len(text) != 2 || text["task"] != "a" || text["reason"] != reason {
			t.Fatalf("note: %v", text)
		}
	}
}
