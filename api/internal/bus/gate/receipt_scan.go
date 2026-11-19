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
// performance problems are fixed when found). They share one scan, kept for
// receiptScanFor. Once a day is read, a refresh reads only what came after it
// and drops what fell out of the day, and Warm keeps it fresh in the
// background, so a cold page load no longer waits for a day's scan.
const (
	receiptScanFor   = time.Minute
	receiptScanFloor = 24 * time.Hour
	receiptScanMax   = 60000
)

type receiptRow struct {
	event, consumer string
	at              time.Time
	seq             uint64
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
			row(receiptRow{m.Header.Get(relay.ReceiptEventHeader), m.Header.Get(relay.ReceiptConsumerHeader), m.At, m.Seq})
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
		// What is kept already covers start: read on from its last receipt.
		incremental := !c.at.IsZero() && !start.Before(c.from) && scanMax <= c.max && len(c.rows) > 0
		var rows []receiptRow
		scanFrom, lastSeq := start, uint64(0)
		if incremental {
			last := c.rows[len(c.rows)-1]
			scanFrom, lastSeq = last.at, last.seq
			for _, r := range c.rows {
				if !r.at.Before(start) {
					rows = append(rows, r)
				}
			}
		}
		err := j.scanReceipts(ctx, scanFrom, scanMax, func(r receiptRow) {
			if !incremental || r.seq > lastSeq {
				rows = append(rows, r)
			}
		})
		if err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
			return nil, false, err
		}
		if !incremental || start.Before(c.from) {
			c.from = start
		} else if f := now.Add(-receiptScanFloor); f.After(c.from) {
			c.from = f
		}
		c.at, c.max, c.rows = now, scanMax, rows
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

// Warm keeps the last day of receipts read every receiptScanFor until ctx ends,
// so the page's reads are answered from memory.
func (j *JSJourney) Warm(ctx context.Context) {
	t := time.NewTicker(receiptScanFor)
	defer t.Stop()
	for {
		wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, _, _ = j.recentReceipts(wctx, time.Now().Add(-receiptScanFloor), receiptScanMax)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
