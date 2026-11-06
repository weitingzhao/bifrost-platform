package controller

import (
	"context"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

func (c *Controller) every(ctx context.Context, wg *sync.WaitGroup, name string, d time.Duration, pass func() error) {
	wg.Add(1)
	safego.Go(name, func() {
		defer wg.Done()
		t := time.NewTicker(d)
		defer t.Stop()
		var run failureRun // the loop's failures in a row (loopalert.go, TD-411)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			var err error
			start := time.Now()
			completed := safego.Do(name, func() {
				if err = pass(); err != nil && ctx.Err() == nil {
					c.logf("%s: %v", name, err)
				}
			})
			c.slow(slowPassLog, start, "%s: pass", name)
			if !completed {
				c.logf("%s: a pass panicked; the loop goes on", name)
				err = errPanicked
			}
			if ctx.Err() == nil {
				c.track(ctx, name, &run, err)
			}
		}
	})
}
