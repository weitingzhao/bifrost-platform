// Package assessmentview bounds optional advice reads on ordinary item lists.
package assessmentview

import (
	"context"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const listBudget = 250 * time.Millisecond
const listConcurrency = 4

// ReadList gives the whole list one short budget, including connection-pool
// waits. Readers must honor ctx; slow reads never hold up the base item response.
// Only this goroutine owns the returned map, so late results cannot mutate it.
func ReadList(ctx context.Context, ids []string, read func(context.Context, string) map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(ids))
	if read == nil || len(ids) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, listBudget)
	defer cancel()
	jobs := make(chan string, len(ids))
	for _, id := range ids {
		if _, exists := out[id]; !exists {
			out[id] = map[string]any{"error": "assessment information is temporarily unavailable"}
			jobs <- id
		}
	}
	close(jobs)
	type result struct {
		id       string
		snapshot map[string]any
	}
	results := make(chan result, listConcurrency)
	for range min(listConcurrency, len(out)) {
		safego.Go("assessmentview.list", func() {
			for id := range jobs {
				if ctx.Err() != nil {
					return
				}
				snapshot := read(ctx, id)
				select {
				case results <- result{id, snapshot}:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	for range len(out) {
		select {
		case res := <-results:
			if ctx.Err() != nil {
				return out
			}
			out[res.id] = res.snapshot
		case <-ctx.Done():
			return out
		}
	}
	return out
}
