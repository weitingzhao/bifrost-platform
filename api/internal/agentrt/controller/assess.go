package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// AssessEvery is how often the assessments pass runs (TD-391): a loop of its
// own, since what it reads outside the runtime (main's commits, the ledger)
// can take tens of seconds.
const AssessEvery = 30 * time.Second

// EvidenceTimeout bounds one assessment's read of the repositories.
const EvidenceTimeout = 45 * time.Second

// Assess is one pass of the assessments (store/assess.go, TD-391): an
// assessment that can no longer come is recorded unavailable; then each card
// the runtime asked and each approval waiting for the Owner that has not been
// asked for one is given to the chief of staff's seat, a few a pass, with
// what the repositories hold about it. It only reads the cards and the
// approvals: nothing here answers a card or decides an approval. It returns
// how many it asked.
func (c *Controller) Assess(ctx context.Context) (int, error) {
	_, eerr := store.EndAssessments(ctx, c.DB)
	if c.Sources.Assessable == nil {
		return 0, eerr
	}
	type list struct {
		items []store.AssessItem
		err   error
	}
	x := bounded(c, ctx, func(ctx context.Context) list { items, err := c.Sources.Assessable(ctx); return list{items, err} })
	errs := []error{eerr}
	if x.err != nil {
		errs = append(errs, fmt.Errorf("what is assessed: %w", x.err))
	}
	asked := 0
	for _, it := range x.items {
		if asked == store.AssessPerPass {
			break
		}
		q, fresh, err := store.AssessmentQuery(ctx, c.DB, it)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", it.Kind, it.ID, err))
			continue
		}
		if !fresh {
			continue
		}
		ev := store.Evidence{NotRead: []string{"the repositories: this platform reads none for assessments"}}
		if c.Sources.Evidence != nil {
			rctx, cancel := context.WithTimeout(ctx, EvidenceTimeout)
			ev = c.Sources.Evidence(rctx, q)
			cancel()
		}
		task, err := store.GiveAssessment(ctx, c.DB, it, ev)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", it.Kind, it.ID, err))
			continue
		}
		if task != "" {
			asked++
		}
	}
	return asked, errors.Join(errs...)
}
