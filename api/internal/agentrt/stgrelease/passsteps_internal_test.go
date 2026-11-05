package stgrelease

import (
	"testing"
	"time"
)

// R5:5.7-5: a slow Pass is told part by part, each part from the end of the
// one before; a fast one says nothing.
func TestPassStepsTellASlowPassPartByPart(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 5, 20, 1, 0, 0, time.UTC)
	p := newPassSteps(t0)
	p.mark("read", t0.Add(300*time.Millisecond))
	p.mark("cleanup", t0.Add(2*time.Second))
	p.mark("sent_ci", t0.Add(6*time.Second))
	p.mark("batch OPEN", t0.Add(7*time.Second))
	if _, ok := p.line(t0.Add(7*time.Second), 10*time.Second); ok {
		t.Fatal("a pass under the threshold was told")
	}
	got, ok := p.line(t0.Add(7*time.Second), 5*time.Second)
	if want := "took 7s: read=300ms cleanup=1.7s sent_ci=4s batch OPEN=1s"; !ok || got != want {
		t.Fatalf("line = %q, %v; want %q", got, ok, want)
	}
}

// Review of R5:5.7-5 round 1: the unblock, block and open-batch paths had no
// mark of their own. Each now marks its part, and whatever follows the last
// mark is told as rest, so a slow path can never hide in the total.
func TestPassStepsTellTheRestAfterTheLastMark(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 5, 21, 0, 0, 0, time.UTC)
	p := newPassSteps(t0)
	p.mark("read", t0.Add(time.Second))
	p.mark("open_batch", t0.Add(3*time.Second))
	got, ok := p.line(t0.Add(9*time.Second), 5*time.Second)
	if want := "took 9s: read=1s open_batch=2s rest=6s"; !ok || got != want {
		t.Fatalf("line = %q, %v; want %q", got, ok, want)
	}
	// Telling the line twice does not grow the parts.
	if again, _ := p.line(t0.Add(9*time.Second), 5*time.Second); again != got {
		t.Fatalf("second line = %q, first %q", again, got)
	}
}
