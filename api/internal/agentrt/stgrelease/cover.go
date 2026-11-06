package stgrelease

import (
	"context"
	"errors"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

type CoverageLedger interface {
	Cover(context.Context, int64, string, store.StgMemberKey, store.MainEvidence) (bool, error)
}

func (l PostgresLedger) Cover(ctx context.Context, seq int64, id string, m store.StgMemberKey, ev store.MainEvidence) (bool, error) {
	return store.CommitStgCoverage(ctx, l.DB, l.Tenant, seq, id, m, ev)
}

// CoverPass covers landed members with archived evidence of any source,
// automatic or external (R5:5.7-11). It reads no manual runs: it needs no
// ExternalExecution on Runs, so it runs every tick whether or not external
// runs are discovered.
func (c *Controller) CoverPass(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.configured() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	s, err := c.ledger().Read(ctx)
	if err != nil {
		return err
	}
	var errs []error
	ledger, ok := c.ledger().(CoverageLedger)
	if ok && c.Sources.ReleaseEvidence != nil {
		members := store.StgCoverageCandidates(s)
		st := coverStats{members: len(members), evidence: len(s.Evidence)}
		start := time.Now()
		defer func() { c.logCover(start, st) }()
		read, done := c.fullReads(&st)
		defer done()
		for _, m := range members {
			for _, evidence := range orderedEvidence(s) {
				id, p := evidence.Record.String("evidence"), evidence.Record.String("P")
				st.pairs++
				// R5:5.7-5: a pair known not to contain R would end below in
				// !HasR; it is skipped. Any other pair, including one that
				// appears after a reread, is read in full as before.
				if c.knownAbsent(p, m.R) {
					st.known++
					continue
				}
				at := c.now()
				st.full++
				ev, err := read(ctx, s, m, p)
				if err != nil {
					st.fullErr++
					errs = append(errs, err)
					continue
				}
				if ev.Head == p && !ev.Unreadable && !ev.HasR {
					c.rememberAbsent(p, m.R) // a complete read: P's history has no R
					st.remembered++
				}
				if ev.Head != p || ev.Unreadable || !ev.HasR || (s.InMain[m.Op] != m.R && ev.Validity != "effective") {
					continue
				}
				if !c.configured() {
					return errors.Join(errs...)
				}
				ev.ReadAt = at
				covered, err := ledger.Cover(ctx, s.LastSeq, id, m, ev)
				if err != nil {
					errs = append(errs, err)
				}
				if covered {
					st.covers++
				}
				s, err = c.ledger().Read(ctx)
				if err != nil {
					return errors.Join(append(errs, err)...)
				}
				break
			}
		}
	}
	// Pass alone settles batches. A terminal archive is no longer occupied,
	// but still owes input verification and the remaining members' budget
	// decision. Coverage must never bypass either by closing the batch here.
	return errors.Join(errs...)
}
