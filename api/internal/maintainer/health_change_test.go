package maintainer

import (
	"reflect"
	"testing"
)

// Origin: 2026-11-18, loop.health.changed (S14): only metrics said how a loop
// was doing. A loop that keeps succeeding must say nothing, or the event would
// be one per pass.
func TestOnlyATurnInALoopsHealthIsTold(t *testing.T) {
	Reset()
	var got []bool
	OnHealthChange(func(_ string, healthy bool) { got = append(got, healthy) })
	defer OnHealthChange(nil)
	for _, r := range []string{ResultSuccess, ResultSuccess, ResultFailure, ResultFailure, ResultSuccess} {
		Note("l", r)
	}
	Note("first-fails", ResultFailure)
	if want := []bool{false, true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
