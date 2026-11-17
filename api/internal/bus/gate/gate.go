// Package gate is the bus's read gate in the api process (RULES.md §8 items
// 2-4): one long poll (GET /api/v1/bus/events), its Server-Sent Events form
// (GET /api/v1/bus/stream) and the journey of one event (GET
// /api/v1/bus/trace). It reads the LEDGER, not NATS: NATS only wakes waiting
// requests sooner, and with NATS absent or down they re-read the ledger every
// two seconds (§8: entry points work without the bus). It imports no domain
// package.
package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Limits of a read.
const (
	MaxWait  = 25 * time.Second
	MaxLimit = 100
	// MaxReplay is how far back since= may start a read (the Bus page's
	// replay of the last day, 2026-11-17).
	MaxReplay = 24 * time.Hour
	// ReplayGrace: a client asking for exactly the last MaxReplay reaches
	// the gate a moment later (STG, 2026-11-17: "since reaches back at most
	// 24h0m0s" for a request of now minus 24 hours).
	ReplayGrace = 5 * time.Minute
)

// Waker wakes everyone waiting at once.
type Waker struct {
	mu sync.Mutex
	ch chan struct{}
}

// C is closed at the next Notify.
func (w *Waker) C() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ch == nil {
		w.ch = make(chan struct{})
	}
	return w.ch
}

// Notify wakes every waiter.
func (w *Waker) Notify() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ch != nil {
		close(w.ch)
	}
	w.ch = make(chan struct{})
}

// Receipts records that a consumer was handed an event (relay.Receipts).
type Receipts interface {
	Publish(ctx context.Context, id, consumer string) error
}

// Journey reads an event's way through NATS for the trace route.
type Journey interface {
	// Published is the stream's message of the event, looked for from the
	// event's ledger time at: its sequence and time.
	Published(ctx context.Context, subject, id string, at time.Time) (seq uint64, stored time.Time, ok bool, err error)
	// Receipts is every receipt of the event, oldest first, looked for from at.
	Receipts(ctx context.Context, id string, at time.Time) ([]ReceiptAt, error)
}

// ReceiptAt is one receipt read back.
type ReceiptAt struct {
	Consumer string    `json:"consumer"`
	At       time.Time `json:"at"`
}

// Gate serves the read routes. Ledger is required; the rest are optional.
type Gate struct {
	Ledger relay.Ledger
	Tenant registry.Tenant
	Env    string
	Wake   *Waker
	// NATSUp says whether a wake can come from NATS; without one the
	// waiters re-read the ledger every Poll.
	NATSUp   func() bool
	Receipts Receipts
	Journey  Journey
	// Since is a read-on position from about a time (eventlog.CursorAt);
	// nil, and since= is refused.
	Since func(ctx context.Context, t time.Time) (relay.Cursor, error)
	// ReceiptFeed passes live receipts to streams asked with receipts=1;
	// nil, and those streams say ": receipts off".
	ReceiptFeed *ReceiptFeed
	Logf        func(format string, args ...any)
	// Clients are the subscription table's declared users (the roster);
	// TablesLoadedAt is when they were loaded.
	Clients        []registry.Client
	TablesLoadedAt time.Time
	seen           seenClients

	// Poll is the re-read interval without NATS (2 s); Heartbeat the SSE
	// comment interval (15 s). Zero is the default.
	Poll, Heartbeat time.Duration
}

func (g *Gate) logf(format string, args ...any) {
	if g.Logf != nil {
		g.Logf(format, args...)
	}
}

func (g *Gate) poll() time.Duration {
	p := g.Poll
	if p <= 0 {
		p = 2 * time.Second
	}
	// With NATS up a wake comes with each published event; the re-read is
	// the safety net only.
	if g.NATSUp != nil && g.NATSUp() {
		p *= 8
	}
	return p
}

type query struct {
	types  []string
	match  string
	after  relay.Cursor
	now    bool // no cursor given: from the horizon
	wait   time.Duration
	limit  int
	client string
	// replay: read from since=, a look back and not a delivery, so it
	// writes no receipts (a day of console receipts would bury the real ones).
	replay bool
}

var clientRE = regexp.MustCompile(`^[\x21-\x7e]{1,40}$`)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (g *Gate) parse(r *http.Request, after string) (query, error) {
	v := r.URL.Query()
	q := query{limit: MaxLimit, client: "anon", match: v.Get("match")}
	if t := strings.TrimSpace(v.Get("types")); t != "" {
		for _, typ := range strings.Split(t, ",") {
			typ = strings.TrimSpace(typ)
			if _, ok := g.Tenant.Channels[typ]; !ok {
				return q, fmt.Errorf("type %q is not registered", typ)
			}
			if !slices.Contains(q.types, typ) {
				q.types = append(q.types, typ)
			}
		}
	} else {
		q.types = g.Tenant.Types()
	}
	if len(q.match) > 200 {
		return q, fmt.Errorf("match is over 200 characters")
	}
	if after == "" {
		q.now = true
	} else if c, ok := relay.ParseCursor(after); ok {
		q.after = c
	} else {
		return q, fmt.Errorf("after %q is not a cursor <txid>.<event_id>", after)
	}
	if s := v.Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		switch {
		case after != "":
			return q, fmt.Errorf("since and a cursor (after, Last-Event-ID) do not go together")
		case err != nil:
			return q, fmt.Errorf("since %q is not an RFC 3339 time", s)
		case time.Since(t) > MaxReplay+ReplayGrace:
			return q, fmt.Errorf("since reaches back at most %s", MaxReplay)
		case g.Since == nil:
			return q, fmt.Errorf("this gate does not read from a time")
		}
		c, err := g.Since(r.Context(), t)
		if err != nil {
			return q, fmt.Errorf("since: %w", err)
		}
		q.after, q.now, q.replay = c, false, true
	}
	if s := v.Get("wait"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 || time.Duration(n)*time.Second > MaxWait {
			return q, fmt.Errorf("wait is whole seconds, 0 to %d", int(MaxWait/time.Second))
		}
		q.wait = time.Duration(n) * time.Second
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > MaxLimit {
			return q, fmt.Errorf("limit is 1 to %d", MaxLimit)
		}
		q.limit = n
	}
	if c := v.Get("client"); c != "" {
		if !clientRE.MatchString(c) {
			return q, fmt.Errorf("client is 1 to 40 printable characters without spaces")
		}
		q.client = c
	}
	return q, nil
}

// read is the events after q.after of q.types whose subject holds q.match,
// waiting up to q.wait for the first, and where to read on from.
func (g *Gate) read(ctx context.Context, q query) ([]relay.Envelope, relay.Cursor, error) {
	cursor := q.after
	if q.now {
		h, err := g.Ledger.Horizon(ctx)
		if err != nil {
			return nil, cursor, err
		}
		cursor = h
	}
	deadline := time.Now().Add(q.wait)
	for {
		var woken <-chan struct{}
		if g.Wake != nil {
			woken = g.Wake.C() // taken before reading: a wake during the read is not lost
		}
		rows, next, err := g.Ledger.After(ctx, cursor, q.limit, q.types)
		if err != nil {
			return nil, cursor, err
		}
		var out []relay.Envelope
		for _, row := range rows {
			ch, ok := g.Tenant.Channels[row.Type]
			if !ok {
				continue
			}
			e, _, err := relay.Build(row, ch, g.Tenant.Name, g.Env)
			if err != nil {
				g.logf("bus gate: event %d passed over: %v", row.EventID, err)
				continue
			}
			if q.match == "" || strings.Contains(e.Subject, q.match) {
				out = append(out, e)
			}
		}
		if cursor.Less(next) {
			cursor = next
		}
		if len(out) > 0 {
			return out, cursor, nil
		}
		if len(rows) == q.limit {
			continue // a full page that matched nothing: read on at once
		}
		left := time.Until(deadline)
		if left <= 0 {
			return []relay.Envelope{}, cursor, nil
		}
		t := time.NewTimer(min(left, g.poll()))
		select {
		case <-ctx.Done():
			t.Stop()
			return []relay.Envelope{}, cursor, ctx.Err()
		case <-woken:
		case <-t.C:
		}
		t.Stop()
	}
}

// receipt records the hand-over of each event, without making the client
// wait for it.
func (g *Gate) receipt(events []relay.Envelope, route, client string) {
	if g.Receipts == nil || len(events) == 0 {
		return
	}
	consumer := "gate:" + route + ":" + client
	safego.Go("bus-gate-receipts", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, e := range events {
			if err := g.Receipts.Publish(ctx, e.ID, consumer); err != nil {
				g.logf("bus gate: receipt of %s: %v", e.ID, err)
				return
			}
		}
	})
}

// HandleEvents is GET /api/v1/bus/events?types=a,b&match=&after=&since=&wait=&limit=&client=.
// since= (RFC 3339, at most MaxReplay back) reads from about that time
// instead of a cursor, and writes no receipts; with receipts=1 it also
// answers the receipts those events had (ReceiptRange).
// It answers {events: [<CloudEvents envelope>], next: "<cursor>"}; after
// empty reads from now. Read-only.
func (g *Gate) HandleEvents(w http.ResponseWriter, r *http.Request) {
	q, err := g.parse(r, r.URL.Query().Get("after"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	events, next, err := g.read(r.Context(), q)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	g.seen.mark("gate:events:"+q.client, time.Now())
	if !q.replay {
		g.receipt(events, "events", q.client)
	}
	out := map[string]any{"events": events, "next": next.String()}
	// A replay may ask for the receipts its events had (receipts=1): read
	// back from the receipt stream once for the page, so the map can land
	// replayed pulses as the live ones landed.
	if q.replay && r.URL.Query().Get("receipts") == "1" && len(events) > 0 {
		rr, ok := g.Journey.(ReceiptRange)
		if !ok {
			out["receipts_note"] = "this gate reads no receipt stream"
		} else if got, err := rr.ReceiptsOf(r.Context(), events); err != nil {
			out["receipts_note"] = "receipts not read: " + err.Error()
		} else {
			out["receipts"] = got
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// HandleStream is GET /api/v1/bus/stream?types=&match=&after=&since=&receipts=&client=, the
// same events as Server-Sent Events: each event's id: is its cursor, so a
// client that reconnects resumes from Last-Event-ID; a comment every
// Heartbeat keeps the connection open. The route keeps the viewer bearer: a
// browser's EventSource cannot send one, so the Console reads it with fetch
// streaming and sends Last-Event-ID itself. With receipts=1 it also passes
// on every live receipt as an "rcpt" event ({id, consumer, at}; no id:
// line, so Last-Event-ID stays the events' cursor).
func (g *Gate) HandleStream(w http.ResponseWriter, r *http.Request) {
	after := r.URL.Query().Get("after")
	if id := strings.TrimSpace(r.Header.Get("Last-Event-ID")); id != "" {
		after = id
	}
	q, err := g.parse(r, after)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	beat := g.Heartbeat
	if beat <= 0 {
		beat = 15 * time.Second
	}
	q.wait = beat
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// One writer at a time: the events below, and the receipts' forwarder.
	var wmu sync.Mutex
	write := func(text string) error {
		wmu.Lock()
		defer wmu.Unlock()
		if _, err := fmt.Fprint(w, text); err != nil {
			return err
		}
		return rc.Flush()
	}
	if write("retry: 2000\n: connected\n\n") != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if r.URL.Query().Get("receipts") == "1" {
		if g.ReceiptFeed == nil {
			if write(": receipts off\n\n") != nil {
				return
			}
		} else {
			notes, stop := g.ReceiptFeed.Listen()
			defer stop()
			safego.Go("bus-gate-stream-receipts", func() {
				for {
					select {
					case <-ctx.Done():
						return
					case n := <-notes:
						data, _ := json.Marshal(n)
						if write("event: rcpt\ndata: "+string(data)+"\n\n") != nil {
							cancel()
							return
						}
					}
				}
			})
		}
	}
	for ctx.Err() == nil {
		events, next, err := g.read(ctx, q)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// The ledger is unreachable: say so, and let the client come back.
			_ = write(": error " + strings.ReplaceAll(err.Error(), "\n", " ") + "\n\n")
			return
		}
		q.after, q.now = next, false
		var b strings.Builder
		if len(events) == 0 {
			b.WriteString(": heartbeat\n\n")
		}
		for _, e := range events {
			data, _ := json.Marshal(e)
			fmt.Fprintf(&b, "id: %s\ndata: %s\n\n", e.Cursor, data)
		}
		if write(b.String()) != nil {
			return
		}
		g.seen.mark("gate:stream:"+q.client, time.Now())
		if !q.replay {
			g.receipt(events, "stream", q.client)
		}
	}
}

// HandleTrace is GET /api/v1/bus/trace?id=ev-<n>: the whole journey of one
// event, each step with its time and the seconds since the step it follows:
// the ledger row, the stream's message (after the ledger), and every receipt
// (after the stream's message, or the ledger when it is not found). Both
// streams are read from 5 s before the event's ledger time to 10 minutes
// after it (at most 5000 messages each): a step outside that window is not
// shown. Read-only.
func (g *Gate) HandleTrace(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	n, ok := relay.ParseEventID(id)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is an event id, ev-<n>"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	row, found, err := g.Ledger.Event(ctx, n)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	ch, registered := g.Tenant.Channels[row.Type]
	if !found || !registered {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": id + " is not an event of a registered type"})
		return
	}
	e, _, err := relay.Build(row, ch, g.Tenant.Name, g.Env)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	type step struct {
		Step    string   `json:"step"`
		At      string   `json:"at"`
		Since   string   `json:"since,omitempty"`   // the step this one is timed from
		Seconds *float64 `json:"seconds,omitempty"` // seconds after that step
		Seq     uint64   `json:"seq,omitempty"`
	}
	secs := func(a, b time.Time) *float64 { s := b.Sub(a).Seconds(); return &s }
	steps := []step{{Step: "ledger", At: e.Time}}
	out := map[string]any{"id": e.ID, "type": e.Type, "subject": e.Subject, "actor": e.Actor, "cursor": e.Cursor,
		"ref": e.Ref, "envelope": e}
	if g.Journey == nil {
		out["nats"] = "not configured: only the ledger's step is known"
		out["steps"] = steps
		writeJSON(w, http.StatusOK, out)
		return
	}
	prev, prevName := row.At, "ledger"
	seq, at, published, err := g.Journey.Published(ctx, relay.EventSubject(g.Tenant.Name, g.Env, row.Type), e.ID, row.At)
	switch {
	case err != nil:
		out["nats"] = "the stream could not be read: " + err.Error()
	case !published:
		out["nats"] = "not on the stream within 10 minutes of the event (not relayed yet, relayed late, or past its retention)"
	default:
		steps = append(steps, step{Step: "stream", At: at.UTC().Format(time.RFC3339Nano), Since: prevName, Seconds: secs(prev, at), Seq: seq})
		prev, prevName = at, "stream"
	}
	receipts, err := g.Journey.Receipts(ctx, e.ID, row.At)
	if err != nil {
		out["receipts_error"] = err.Error()
	}
	for _, rc := range receipts {
		steps = append(steps, step{Step: "receipt " + rc.Consumer, At: rc.At.UTC().Format(time.RFC3339Nano), Since: prevName, Seconds: secs(prev, rc.At)})
	}
	out["steps"] = steps
	writeJSON(w, http.StatusOK, out)
}
