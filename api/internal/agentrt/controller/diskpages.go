package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// DiskPagesEvery is how often the controller pages the Owner of hosts' disk
// records (TD-415): a page the relay did not take is tried again a minute
// later, a relay away for longer is a loop failing (loopalert.go).
const DiskPagesEvery = time.Minute

// diskPagesPerPass bounds a pass; diskPageLimit bounds one page.
const (
	diskPagesPerPass = 10
	diskPageLimit    = 15 * time.Second
)

// DiskPages is one pass paging the Owner of the hosts' disk records not
// paged yet (store.UnpagedDiskChanges), oldest first: one the relay took, or
// has no target for, is marked paged with that result; one it failed is left
// for the next pass, and the pass says so (review 497 A4). It returns how
// many it marked.
func (c *Controller) DiskPages(ctx context.Context) (int, error) {
	if c.PageOwner == nil {
		return 0, nil
	}
	pages, err := store.UnpagedDiskChanges(ctx, c.DB, diskPagesPerPass)
	if err != nil {
		return 0, err
	}
	marked := 0
	var errs []error
	for _, p := range pages {
		pctx, cancel := context.WithTimeout(ctx, diskPageLimit)
		result, perr := c.PageOwner(pctx, p.Title, p.Text, fmt.Sprintf("disk:%s:%d", p.Host, p.EventID))
		cancel()
		if perr != nil {
			errs = append(errs, fmt.Errorf("page of host %s's disk (event %d): %w", p.Host, p.EventID, perr))
			continue
		}
		if err = store.MarkDiskPaged(ctx, c.DB, p, result); err != nil {
			errs = append(errs, err)
			continue
		}
		marked++
	}
	return marked, errors.Join(errs...)
}
