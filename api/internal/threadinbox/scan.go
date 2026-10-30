// Package threadinbox derives delivery from business records. It never writes
// statefiles; the sender's PostgreSQL transaction owns messages and receipts.
package threadinbox

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sender commits a message and its permanent receipt together. Missing or
// retired actors are successful skips; storage errors are retryable.
type Sender func(ctx context.Context, key, to, body string) error

type Conclusion struct {
	Key, To, Body string
}

// Scan keeps only a business-record scheduling cursor in memory, never a
// delivery receipt or a copy of a deleted record. A restart scans from the start.
// Serializing scans does not acquire the service/business-state mutex.
type Scan struct {
	mu    sync.Mutex
	after string
}

// Run shares one budget across discovery and sending so existing result
// recovery and sweep loops are not delayed by the size of the backlog.
func (s *Scan) Run(ctx context.Context, load func(context.Context) ([]Conclusion, error), send Sender) error {
	if !s.mu.TryLock() {
		return nil
	}
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	items, err := load(ctx)
	if err != nil {
		return err
	}
	sort.Slice(items, func(i, j int) bool {
		if a, b := recordKey(items[i]), recordKey(items[j]); a != b {
			return a < b
		}
		return scanKey(items[i]) < scanKey(items[j])
	})
	var records [][]Conclusion
	for i := 0; i < len(items); {
		end := i + 1
		for end < len(items) && recordKey(items[end]) == recordKey(items[i]) {
			end++
		}
		records = append(records, items[i:end])
		i = end
	}
	start := sort.Search(len(records), func(i int) bool { return recordKey(records[i][0]) > s.after })
	for n := 0; n < len(records); n++ {
		record := records[(start+n)%len(records)]
		for _, item := range record {
			if err := ctx.Err(); err != nil {
				return err
			}
			// Advance past the whole record on failure to preserve fairness.
			// Its next attempt starts with unknown again; only the sender's
			// permanent receipt permits progressing to the final conclusion.
			s.after = recordKey(item)
			if err := send(ctx, item.Key, item.To, item.Body); err != nil {
				return err
			}
		}
	}
	return nil
}

// Keys are <business kind>:<record id>:<state>; only the last component is
// omitted, keeping approval/question namespaces and ids distinct.
func recordKey(c Conclusion) string {
	if i := strings.LastIndexByte(c.Key, ':'); i >= 0 {
		return c.Key[:i]
	}
	return c.Key
}

// A late result's existing business marker also proves an unknown state.
// When both are reconstructed, try that earlier conclusion first.
func scanKey(c Conclusion) string {
	if key, ok := strings.CutSuffix(c.Key, ":unknown"); ok {
		return key + ":"
	}
	return c.Key
}
