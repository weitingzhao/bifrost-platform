package gate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

type countReceipts struct {
	mu sync.Mutex
	n  int
}

func (c *countReceipts) Publish(context.Context, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return nil
}

func (c *countReceipts) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// Origin: 2026-11-17, the Bus page's nerve map (the coordinator, relaying
// the Owner): with two or three events a minute the map stands still most of
// the time, so it replays the last day from the ledger. since= reads from
// the cursor the ledger gives for that time; it writes no receipts (a
// replay is a look back, not a delivery); and it is refused with a cursor,
// past MaxReplay, or where the gate cannot read from a time.
func TestSinceReplaysFromATimeWithoutReceipts(t *testing.T) {
	l := &fakeLedger{horizon: 50}
	l.add(gitRow(7, 20, "bifrost-platform"))
	l.add(gitRow(8, 30, "bifrost-platform"))
	g := newGate(t, l)
	rc := &countReceipts{}
	g.Receipts = rc
	var asked time.Time
	g.Since = func(_ context.Context, at time.Time) (relay.Cursor, error) {
		asked = at
		return relay.Cursor{TxID: 25}, nil
	}
	since := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	// Exactly MaxReplay back, as the page asks, a moment before it arrives:
	// accepted (STG, 2026-11-17, the replay was refused for this).
	full := time.Now().Add(-MaxReplay).UTC().Format(time.RFC3339)
	if a, _ := get(t, g, "types=git.main.advanced&wait=0&since="+full); a.Next == "" {
		t.Fatalf("a full day back was refused: %+v", a)
	}
	a, _ := get(t, g, "types=git.main.advanced&wait=0&since="+since.Format(time.RFC3339))
	if !asked.Equal(since) || len(a.Events) != 1 || a.Events[0].ID != "ev-8" {
		t.Fatalf("since %s (asked %s): %+v", since, asked, a)
	}
	// A live read of the same events writes their receipts (positive control).
	get(t, g, "types=git.main.advanced&wait=0&after=0.0")
	time.Sleep(200 * time.Millisecond)
	live := rc.count()
	if live == 0 {
		t.Fatal("a live read wrote no receipts")
	}
	get(t, g, "types=git.main.advanced&wait=0&since="+since.Format(time.RFC3339))
	time.Sleep(200 * time.Millisecond)
	if rc.count() != live {
		t.Fatalf("a replay wrote receipts: %d after %d", rc.count(), live)
	}

	for name, q := range map[string]string{
		"with a cursor": "after=0.0&since=" + since.Format(time.RFC3339),
		"too far back":  "since=" + time.Now().Add(-MaxReplay-ReplayGrace-time.Minute).UTC().Format(time.RFC3339),
		"not a time":    "since=yesterday",
	} {
		rec := httptest.NewRecorder()
		g.HandleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bus/events?types=git.main.advanced&wait=0&"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d", name, rec.Code)
		}
	}
	g.Since = nil
	rec := httptest.NewRecorder()
	g.HandleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bus/events?types=git.main.advanced&wait=0&since="+since.Format(time.RFC3339), nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("without Since: HTTP %d", rec.Code)
	}
}

type rangeJourney struct{ Journey }

func (rangeJourney) ReceiptsOf(_ context.Context, events []relay.Envelope) ([]ReceiptNote, error) {
	out := make([]ReceiptNote, 0, len(events))
	for _, e := range events {
		out = append(out, ReceiptNote{ID: e.ID, Consumer: "headrelease", At: time.Now()})
	}
	return out, nil
}

// Origin: 2026-11-17, the coordinator on STG: replayed pulses stopped at
// their topic because receipts were not replayed. A since= read asked with
// receipts=1 answers the receipts its events had; not asked, it answers
// none (the control); a gate whose journey reads no receipt stream says so.
func TestSinceAnswersReceiptsWhenAsked(t *testing.T) {
	l := &fakeLedger{horizon: 50}
	l.add(gitRow(8, 30, "bifrost-platform"))
	g := newGate(t, l)
	g.Since = func(context.Context, time.Time) (relay.Cursor, error) { return relay.Cursor{TxID: 25}, nil }
	g.Journey = rangeJourney{}
	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	body := func(q string) map[string]any {
		rec := httptest.NewRecorder()
		g.HandleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bus/events?types=git.main.advanced&wait=0&since="+since+q, nil))
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if rs, _ := body("&receipts=1")["receipts"].([]any); len(rs) != 1 {
		t.Fatalf("asked, receipts %v", rs)
	}
	if _, ok := body("")["receipts"]; ok {
		t.Fatal("not asked, receipts came")
	}
	g.Journey = nil
	if n, _ := body("&receipts=1")["receipts_note"].(string); n == "" {
		t.Fatal("no receipt stream, not said")
	}
}
