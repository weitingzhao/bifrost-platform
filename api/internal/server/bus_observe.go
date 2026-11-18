package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gate"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/observe"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// observeCardTypes are the card events the picture reads from the ledger.
var observeCardTypes = []string{"approval.card.created", "approval.card.decided"}

// GET /api/v1/bus/observe?hours=: the team at work for the Bus page's nerve
// map (bus/observe): who wrote to whom and how fast it was taken, the seats'
// last status reports, how long the cards waited, and what is not recorded.
// Read when asked; the page reads it with its other once-a-minute reads.
func (s *Server) handleBusObserve(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 168 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hours is 1 to 168"})
			return
		}
		hours = n
	}
	pool := s.bus.pool.Load()
	if pool == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the bus's database is not connected"})
		return
	}
	now := time.Now()
	since := now.Add(-time.Duration(hours) * time.Hour)
	letters, status, err := observe.ReadLetters(r.Context(), pool, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ledger := &relay.PGLedger{DB: func() relay.TxBeginner { return pool }, EventsAfter: busEventsAfter}
	cards, err := observeCards(r.Context(), ledger, busCursorAt(pool), since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	p := observe.Build(now, hours, letters, status, cards)
	if tenants, err := registry.Load(); err == nil {
		if t, ok := tenants[relay.Tenant]; ok {
			if types, err := observeTypes(r.Context(), ledger, busCursorAt(pool), now.Add(-time.Hour), t.Types()); err == nil {
				p.Rate = observe.NewRate(60, types)
			}
			if l, err := s.observeListening(r.Context(), ledger, busCursorAt(pool), now, t); err == nil {
				p.Listening = &l
			}
		}
	}
	writeJSON(w, http.StatusOK, p)
}

// listenHours is the window of the listening readings: the last day.
const listenHours = 24

// listenScan bounds the receipt messages one reading scans (a day of PROD is some thousands).
const listenScan = 60000

// observeListening reads whether anyone listens: the last day's bus events
// (ledger), who took each (one scan of the receipt stream), and who may read
// each topic (declared consumers and subscription rows).
func (s *Server) observeListening(ctx context.Context, l relay.Ledger, at func(context.Context, time.Time) (relay.Cursor, error), now time.Time, t registry.Tenant) (observe.Listening, error) {
	since := now.Add(-listenHours * time.Hour)
	c, err := at(ctx, since)
	if err != nil {
		return observe.Listening{}, err
	}
	var events []observe.ListenEvent
	for page := 0; page < 200; page++ {
		rows, next, err := l.After(ctx, c, 100, t.Types())
		if err != nil {
			return observe.Listening{}, err
		}
		for _, row := range rows {
			if !row.At.Before(since) {
				events = append(events, observe.ListenEvent{ID: relay.EventID(row.EventID), Type: row.Type, Cause: row.Cause, N: row.EventID})
			}
		}
		if len(rows) < 100 || next == c {
			break
		}
		c = next
	}
	readers := map[string][]string{}
	display := map[string]bool{}
	for typ, ch := range t.Channels {
		readers[typ] = append(readers[typ], ch.Consumers...)
		display[typ] = ch.DisplayOnly
	}
	if subs, err := registry.LoadSubscriptions(); err == nil {
		for _, set := range subs {
			for _, row := range set.Rows {
				for _, on := range row.On {
					readers[on] = append(readers[on], "row:"+row.ID)
				}
			}
		}
	}
	var receipts map[string][]string
	read := false
	var cut bool
	if g := s.bus.gate.Load(); g != nil {
		if j, ok := g.Journey.(*gate.JSJourney); ok {
			if receipts, cut, err = j.ReceiptsByEvent(ctx, since, listenScan); err == nil {
				read = true
			}
		}
	}
	out := observe.BuildListening(listenHours, events, receipts, read, readers, "gate:stream:console", display)
	if cut {
		out.Notes = append(out.Notes, "the receipt scan stopped at its limit: the heard share is a floor")
	}
	return out, nil
}

// observeTypes is the type of every bus event since a time, a page at a time.
func observeTypes(ctx context.Context, l relay.Ledger, at func(context.Context, time.Time) (relay.Cursor, error), since time.Time, types []string) ([]string, error) {
	c, err := at(ctx, since)
	if err != nil {
		return nil, err
	}
	var out []string
	for page := 0; page < 50; page++ {
		rows, next, err := l.After(ctx, c, 100, types)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !row.At.Before(since) {
				out = append(out, row.Type)
			}
		}
		if len(rows) < 100 || next == c {
			break
		}
		c = next
	}
	return out, nil
}

// observeCards reads the cards' created and decided events since a time,
// a page at a time (a week is some dozens).
func observeCards(ctx context.Context, l relay.Ledger, at func(context.Context, time.Time) (relay.Cursor, error), since time.Time) ([]observe.Card, error) {
	c, err := at(ctx, since)
	if err != nil {
		return nil, err
	}
	byID := map[string]*observe.Card{}
	var order []string
	for page := 0; page < 50; page++ {
		rows, next, err := l.After(ctx, c, 100, observeCardTypes)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			id, _ := row.Body["id"].(string)
			if id == "" {
				continue
			}
			card, ok := byID[id]
			if !ok {
				card = &observe.Card{ID: id}
				byID[id] = card
				order = append(order, id)
			}
			switch row.Type {
			case "approval.card.created":
				card.Created = row.At
			case "approval.card.decided":
				card.Decided = row.At
			}
		}
		if len(rows) < 100 || next == c {
			break
		}
		c = next
	}
	out := make([]observe.Card, 0, len(order))
	for _, id := range order {
		if card := byID[id]; !card.Created.IsZero() {
			out = append(out, *card)
		}
	}
	return out, nil
}
