package gate

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Origin: 2026-11-17, the Bus page's nerve map: a pulse lands on its reader
// when the reader's receipt comes, so the one stream the page reads passes
// the live receipts on when asked (receipts=1), as "rcpt" events with no id
// (Last-Event-ID stays the events' cursor). Not asked, none come (the
// control); asked where the gate follows no receipts, it says so.
func TestStreamPassesReceiptsWhenAsked(t *testing.T) {
	l := &fakeLedger{horizon: 50}
	g := newGate(t, l)
	g.Heartbeat, g.Poll = 100*time.Millisecond, 50*time.Millisecond
	feed := &ReceiptFeed{}
	g.ReceiptFeed = feed
	srv := httptest.NewServer(http.HandlerFunc(g.HandleStream))
	defer srv.Close()

	// lines reads the stream for a while, noting a receipt once it is connected.
	lines := func(query string) []string {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?types=git.main.advanced&"+query, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out []string
		sc := bufio.NewScanner(resp.Body)
		noted := false
		for sc.Scan() {
			out = append(out, sc.Text())
			if !noted && sc.Text() == ": connected" {
				noted = true
				go func() {
					time.Sleep(200 * time.Millisecond)
					feed.Note(ReceiptNote{ID: "ev-7", Consumer: "seat:ci", At: time.Now()})
				}()
			}
		}
		return out
	}
	got := strings.Join(lines("receipts=1"), "\n")
	if !strings.Contains(got, "event: rcpt\ndata: {\"id\":\"ev-7\",\"consumer\":\"seat:ci\"") {
		t.Fatalf("asked, no receipt:\n%s", got)
	}
	if got := strings.Join(lines(""), "\n"); strings.Contains(got, "rcpt") {
		t.Fatalf("not asked, a receipt came:\n%s", got)
	}
	g.ReceiptFeed = nil
	if got := strings.Join(lines("receipts=1"), "\n"); !strings.Contains(got, ": receipts off") {
		t.Fatalf("no feed, not said:\n%s", got)
	}
}
