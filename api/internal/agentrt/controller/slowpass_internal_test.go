package controller

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// R5:5.7-5: a slow pass or landing operation leaves one line with its time;
// a fast one leaves nothing.
func TestSlowLogsOnlyWhatTookLong(t *testing.T) {
	t.Parallel()
	var got []string
	c := &Controller{Logf: func(f string, a ...any) { got = append(got, fmt.Sprintf(f, a...)) }}
	if c.slow(time.Hour, time.Now(), "agentrt-landing-drill: pass") {
		t.Fatal("logged a pass faster than the threshold")
	}
	if !c.slow(time.Second, time.Now().Add(-2*time.Second), "agentrt-landing-drill: %s %s", "bifrost-platform", "land:t_1:1") {
		t.Fatal("did not log a pass slower than the threshold")
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "agentrt-landing-drill: bifrost-platform land:t_1:1 took 2") || !strings.HasSuffix(got[0], "s") {
		t.Fatalf("logged %q", got)
	}
}
