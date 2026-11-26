package cidomains

import (
	"context"
	"log/slog"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// aheadEvery is how often the workers look whether main moved.
const aheadEvery = time.Minute

// ReadAhead reads the domain graph before anybody asks: once a minute, in the
// workers, it asks for the graph of each repository, which starts the read of a
// new head and keeps the result in the platform's state; the API then answers
// from what is kept.
//
// 事故 2026-11-16 22:5x: the graph was computed on the click, so the
// reader waited. A click should only read. This is
// the plain form (a look a minute); waking on git.main.advanced instead is
// the communication line's bus, and can replace the ticker.
func (c *Cache) ReadAhead(ctx context.Context, repos []string, source func() Source) {
	safego.Go("ci-domains-ahead", func() {
		t := time.NewTicker(aheadEvery)
		defer t.Stop()
		for {
			for _, repo := range repos {
				pass, cancel := context.WithTimeout(kubemetrics.WithSite(ctx, "ci-domains-ahead"), 20*time.Second)
				if _, err := c.Graph(pass, source(), repo); err != nil && ctx.Err() == nil {
					slog.Debug("ci domains ahead", "repo", repo, "error", err)
				}
				cancel()
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	})
}
