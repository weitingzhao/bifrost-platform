package gate

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Origin: 2026-11-19, the Owner's "performance problems are fixed when found":
// the Bus page's two slowest reads each scanned a day of receipts. One scan must
// serve both, and a failure or an older window must not be answered from it.
func TestReceiptReadsShareOneScan(t *testing.T) {
	now := time.Now()
	scans := 0
	var fail error
	j := &JSJourney{scan: func(_ context.Context, from time.Time, _ int, row func(receiptRow)) error {
		scans++
		if fail != nil {
			return fail
		}
		for _, r := range []receiptRow{
			{"ev-1", "a", now.Add(-30 * time.Hour)}, {"ev-2", "a", now.Add(-2 * time.Hour)}, {"ev-2", "b", now.Add(-time.Minute)},
		} {
			if !r.at.Before(from) {
				row(r)
			}
		}
		return nil
	}}
	ctx := context.Background()
	byEvent, cut, err := j.ReceiptsByEvent(ctx, now.Add(-24*time.Hour), 100)
	if err != nil || cut || len(byEvent["ev-2"]) != 2 || byEvent["ev-1"] != nil {
		t.Fatalf("by event %v, cut %v, %v", byEvent, cut, err)
	}
	counts, err := j.ReceiptsSince(ctx, now.Add(-time.Hour), 100)
	if err != nil || scans != 1 || counts["b"].Count != 1 || counts["a"].Count != 0 {
		t.Fatalf("since: %v, %d scans, %v", counts, scans, err)
	}
	if counts, err = j.ReceiptsSince(ctx, now.Add(-48*time.Hour), 100); err != nil || scans != 2 || counts["a"].Count != 2 {
		t.Fatalf("an older window: %v, %d scans, %v", counts, scans, err)
	}
	if _, cut, _ = j.ReceiptsByEvent(ctx, now.Add(-48*time.Hour), 2); !cut {
		t.Fatal("a read over its limit does not say it was cut")
	}
	j.receipts.at = now.Add(-2 * receiptScanFor)
	fail = errors.New("stream down")
	if _, err = j.ReceiptsSince(ctx, now.Add(-time.Hour), 100); err == nil {
		t.Fatal("a failed scan answered")
	}
	fail = nil
	if counts, err = j.ReceiptsSince(ctx, now.Add(-time.Hour), 100); err != nil || counts["b"].Count != 1 {
		t.Fatalf("after a failure: %v, %v", counts, err)
	}
}
