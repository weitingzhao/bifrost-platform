package stgrelease

import (
	"fmt"
	"log/slog"
	"time"
)

// R5:5.7-5. While a landed member waits for STG, ExternalPass asks every
// archived evidence, newest first, whether it covers the member, each with a
// full ReleaseEvidence read (a fresh workspace, two fetches). 11-05: from the
// landing on, ticks 15 s apart went 3 minutes apart, the external pass's
// budget, on every tick until the member was delivered. Most pairs cannot
// cover: R is not in P's history and the read ends in !HasR. Such a pair,
// once read in full, is remembered and skipped on later passes. Nothing else
// changes: the loop, its order, its break and every check on a read that may
// cover stay as they were, and a pair not remembered is read in full.

// containKey is a pair whose P does not contain R: P is the evidence's
// Record "P", R the candidate's m.R. Commits are content addressed: P's
// ancestors never change, so neither does the answer. Only a complete read
// says so: no error, Head == P, not Unreadable, HasR false.
type containKey struct{ p, r string }

// absentMax bounds the remembered pairs; past it the memory starts over and
// the pairs are read in full again (a lost optimisation, never a wrong skip).
const absentMax = 10000

// coverSample: a coverage pass is logged when it is slow, and otherwise at
// most once in this long, so fast passes are seen to run too.
const coverSample = 10 * time.Minute

func (c *Controller) knownAbsent(p, r string) bool { return c.absent[containKey{p, r}] }

func (c *Controller) rememberAbsent(p, r string) {
	k := containKey{p, r}
	if c.absent[k] {
		return
	}
	if c.absent == nil || len(c.absent) >= absentMax {
		c.absent = map[containKey]bool{}
	}
	c.absent[k] = true
}

// coverStats is what one coverage pass did, for its log line.
type coverStats struct {
	members, evidence, pairs, known, full, fullErr, remembered, covers int
}

func (s coverStats) String() string {
	return fmt.Sprintf("members=%d evidence=%d pairs=%d known_absent=%d full_reads=%d full_errors=%d remembered=%d covers=%d",
		s.members, s.evidence, s.pairs, s.known, s.full, s.fullErr, s.remembered, s.covers)
}

// logCover writes the pass's line when it was slow, or when the last sampled
// line is older than coverSample.
func (c *Controller) logCover(start time.Time, st coverStats) {
	took := time.Since(start)
	now := time.Now()
	if took < slowPassLog && now.Sub(c.coverLogged) < coverSample {
		return
	}
	c.coverLogged = now
	slog.Info("STG external coverage took "+took.Round(time.Millisecond).String(), "reads", st.String(), "remembered_total", len(c.absent))
}
