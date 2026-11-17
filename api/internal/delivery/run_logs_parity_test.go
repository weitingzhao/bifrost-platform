//bifrost:owner delivery

package delivery

import (
	"reflect"
	"testing"
	"time"
)

func TestLogProjectionCompleteParity(t *testing.T) {
	for _, raw := range []string{"", "plain\n\n", "2026-11-16T12:00:00Z one\n2026-11-16T11:00:00.123Z two\n", "badZ value\n", "2026-11-16T12:00:00+01:00 untouched", "\n2026-11-16T12:00:00.999999999Z next"} {
		got, last := stripK8sLogTimestamps([]byte(raw))
		want, wlast := baselineStripK8sLogTimestamps([]byte(raw))
		if got != want || !reflect.DeepEqual(last, wlast) {
			t.Fatalf("input=%q got=%q/%v want=%q/%v", raw, got, last, want, wlast)
		}
	}
	a, b := time.Unix(1, 0), time.Unix(2, 0)
	for _, x := range []*time.Time{nil, &a, &b} {
		for _, y := range []*time.Time{nil, &a, &b} {
			if maxTimePtr(x, y) != baselineMaxTimePtr(x, y) {
				t.Fatal("pointer selection changed")
			}
		}
	}
}
