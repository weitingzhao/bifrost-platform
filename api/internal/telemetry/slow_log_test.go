package telemetry

import (
	"fmt"
	"testing"
	"time"
)

func TestSlowLogOutputAndBoundary(t *testing.T) {
	for _, d := range []time.Duration{-time.Second, 0, time.Second - time.Nanosecond, time.Second, time.Second + 1500*time.Microsecond} {
		var got []string
		logged := LogSlow(d, time.Second, func(f string, a ...any) { got = append(got, fmt.Sprintf(f, a...)) }, "pass %s %d", "test", 2)
		if logged != (d >= time.Second) {
			t.Fatal(d)
		}
		if logged && (len(got) != 1 || got[0] != fmt.Sprintf("pass test 2 took %s", d.Round(time.Millisecond))) {
			t.Fatal(got)
		}
		if !logged && len(got) != 0 {
			t.Fatal(got)
		}
	}
}
