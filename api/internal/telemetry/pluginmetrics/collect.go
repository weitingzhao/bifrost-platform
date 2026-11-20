package pluginmetrics

// collectProbes gathers independent read-only observations without knowing
// their domains (folded in from telemetry/probecollect 2026-11-20, unchanged
// but for its unexported name).

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"sync"
	"time"
)

// collectProbes runs each observation with its own timeout and preserves input order.
// A contained panic leaves the observation's zero value, as the original scrape did.
// Cancellation remains cooperative: observation functions must respect their context.
func collectProbes[T any](parent context.Context, timeout time.Duration, recoveryName string, probes []func(context.Context) T) []T {
	healths := make([]T, len(probes))
	var wg sync.WaitGroup
	for i, fn := range probes {
		wg.Add(1)
		go func(i int, fn func(context.Context) T) {
			defer safego.Recover(recoveryName)
			defer wg.Done()
			ctx, cancel := context.WithTimeout(parent, timeout)
			defer cancel()
			healths[i] = fn(ctx)
		}(i, fn)
	}
	wg.Wait()
	return healths
}
