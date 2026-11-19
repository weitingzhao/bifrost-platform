package gate

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// The Bus page opens with two reads that each scanned a day of the receipt
// stream: /bus/observe 1.8 s and /bus/clients 1.2 s (2026-11-19, the Owner:
// performance problems are fixed when found). They now share one scan, kept for
// receiptScanFor, which is shorter than either panel's refresh.
const (
	receiptScanFor   = time.Minute
	receiptScanFloor = 24 * time.Hour
	receiptScanMax   = 60000
)

type receiptRow struct {
	event, consumer string
	at              time.Time
}

type receiptScan struct {
	mu   sync.Mutex
	at   time.Time
	from time.Time
	max  int
	rows []receiptRow
}

// scanReceipts reads the stream from a time; a test replaces it.
func (j *JSJourney) scanReceipts(ctx context.Context, from time.Time, max int, row func(receiptRow)) error {
	if j.scan != nil {
		return j.scan(ctx, from, max, row)
	}
	return relay.ScanFrom(ctx, j.JS, relay.ReceiptStreamName(j.Tenant, j.Env), relay.ReceiptSubject(j.Tenant, j.Env),
		from, time.Now().Add(time.Minute), max, func(m relay.StreamMsg) bool {
			row(receiptRow{m.Header.Get(relay.ReceiptEventHeader), m.Header.Get(relay.ReceiptConsumerHeader), m.At})
			return false
		})
}

// recentReceipts is the receipts stored since from, at most max, and whether the
// limit cut them. Readers at the same moment wait for one scan. A failed scan
// is not kept.
func (j *JSJourney) recentReceipts(ctx context.Context, from time.Time, limit int) ([]receiptRow, bool, error) {
	c := &j.receipts
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.at.IsZero() || now.Sub(c.at) >= receiptScanFor || from.Before(c.from) || limit > c.max {
		start := from
		if floor := now.Add(-receiptScanFloor); floor.Before(start) {
			start = floor
		}
		scanMax := max(limit, receiptScanMax)
		var rows []receiptRow
		err := j.scanReceipts(ctx, start, scanMax, func(r receiptRow) { rows = append(rows, r) })
		if err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
			return nil, false, err
		}
		c.at, c.from, c.max, c.rows = now, start, scanMax, rows
	}
	var out []receiptRow
	for _, r := range c.rows {
		if r.at.Before(from) {
			continue
		}
		if len(out) == limit {
			return out, true, nil
		}
		out = append(out, r)
	}
	return out, len(out) == limit, nil
}
