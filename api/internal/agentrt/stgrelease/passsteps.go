package stgrelease

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// slowPassLog: a Pass that takes at least this long leaves one log line with
// the time of each of its parts (R5:5.7-5). 11-05 a batch spent 13 minutes
// between OPEN and its run's start and about 2.5 minutes between the run's
// end and terminal while the loop ticks every 15 seconds: the minutes go
// inside passes, and this says where. Logging only; nothing waits on it.
var slowPassLog = 5 * time.Second

// passSteps times the parts of one Pass, each from the end of the one before.
type passSteps struct {
	start, last time.Time
	parts       []string
}

func newPassSteps(now time.Time) *passSteps { return &passSteps{start: now, last: now} }

// mark closes the part that ended now under name.
func (p *passSteps) mark(name string, now time.Time) {
	p.parts = append(p.parts, fmt.Sprintf("%s=%s", name, now.Sub(p.last).Round(time.Millisecond)))
	p.last = now
}

// line is the log line for a pass that ended now, when it took at least min.
// Time after the last mark is told as rest, so the parts always add up to the
// total: a path without a mark of its own still shows where its time went.
func (p *passSteps) line(now time.Time, min time.Duration) (string, bool) {
	total := now.Sub(p.start)
	if total < min {
		return "", false
	}
	parts := p.parts
	if rest := now.Sub(p.last); rest >= time.Millisecond {
		parts = append(slices.Clone(parts), "rest="+rest.Round(time.Millisecond).String())
	}
	return fmt.Sprintf("took %s: %s", total.Round(time.Millisecond), strings.Join(parts, " ")), true
}

// done logs the pass when it was slow.
func (p *passSteps) done() {
	if l, ok := p.line(time.Now(), slowPassLog); ok {
		slog.Info("STG release pass " + l)
	}
}
