package observe

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gate"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// Handler is the route's dependencies, from the server's wiring: the
// ledger's pool (nil while it is not connected), the ledger read over it,
// the cursor of a time in it, and the bus gate (nil while it is off).
type Handler struct {
	Pool     func() *pgxpool.Pool
	Ledger   func(*pgxpool.Pool) relay.Ledger
	CursorAt func(*pgxpool.Pool) func(context.Context, time.Time) (relay.Cursor, error)
	Gate     func() *gate.Gate
}

// observeCardTypes are the card events the picture reads from the ledger.
var observeCardTypes = []string{"approval.card.created", "approval.card.decided"}

// GET /api/v1/bus/observe?hours=: the team at work for the Bus page's nerve
// map (bus/observe): who wrote to whom and how fast it was taken, the seats'
// last status reports, how long the cards waited, and what is not recorded.
// Read when asked; the page reads it with its other once-a-minute reads.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 168 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hours is 1 to 168"})
			return
		}
		hours = n
	}
	pool := h.Pool()
	if pool == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the bus's database is not connected"})
		return
	}
	now := time.Now()
	since := now.Add(-time.Duration(hours) * time.Hour)
	letters, status, err := ReadLetters(r.Context(), pool, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ledger := h.Ledger(pool)
	cards, err := observeCards(r.Context(), ledger, h.CursorAt(pool), since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	p := Build(now, hours, letters, status, cards)
	if tenants, err := registry.Load(); err == nil {
		if t, ok := tenants[relay.Tenant]; ok {
			if types, err := observeTypes(r.Context(), ledger, h.CursorAt(pool), now.Add(-time.Hour), t.Types()); err == nil {
				p.Rate = NewRate(60, types)
			}
			if l, err := h.listening(r.Context(), ledger, h.CursorAt(pool), now, t); err == nil {
				p.Listening = &l
			}
		}
	}
	writeJSON(w, http.StatusOK, p)
}

// observePage is how many ledger events one read takes: a day is a few
// thousand, so a handful of reads instead of dozens (2026-11-19: /bus/observe
// still took about a second once its receipt scan was warm).
const observePage = 1000

// listenHours is the window of the listening readings: the last day.
const listenHours = 24

// listenScan bounds the receipt messages one reading scans (a day of PROD is some thousands).
const listenScan = 60000

// observeListening reads whether anyone listens: the last day's bus events
// (ledger), who took each (one scan of the receipt stream), and who may read
// each topic (declared consumers and subscription rows).
func (h Handler) listening(ctx context.Context, l relay.Ledger, at func(context.Context, time.Time) (relay.Cursor, error), now time.Time, t registry.Tenant) (Listening, error) {
	since := now.Add(-listenHours * time.Hour)
	c, err := at(ctx, since)
	if err != nil {
		return Listening{}, err
	}
	var events []ListenEvent
	for page := 0; page < 200; page++ {
		rows, next, err := l.After(ctx, c, observePage, t.Types())
		if err != nil {
			return Listening{}, err
		}
		for _, row := range rows {
			if !row.At.Before(since) {
				events = append(events, ListenEvent{ID: relay.EventID(row.EventID), Type: row.Type, Cause: row.Cause, N: row.EventID})
			}
		}
		if len(rows) < observePage || next == c {
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
	if g := h.Gate(); g != nil {
		if j, ok := g.Journey.(*gate.JSJourney); ok {
			if receipts, cut, err = j.ReceiptsByEvent(ctx, since, listenScan); err == nil {
				read = true
			}
		}
	}
	out := BuildListening(listenHours, events, receipts, read, readers, "gate:stream:console", display)
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
		rows, next, err := l.After(ctx, c, observePage, types)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !row.At.Before(since) {
				out = append(out, row.Type)
			}
		}
		if len(rows) < observePage || next == c {
			break
		}
		c = next
	}
	return out, nil
}

// observeCards reads the cards' created and decided events since a time,
// a page at a time (a week is some dozens).
func observeCards(ctx context.Context, l relay.Ledger, at func(context.Context, time.Time) (relay.Cursor, error), since time.Time) ([]Card, error) {
	c, err := at(ctx, since)
	if err != nil {
		return nil, err
	}
	byID := map[string]*Card{}
	var order []string
	for page := 0; page < 50; page++ {
		rows, next, err := l.After(ctx, c, observePage, observeCardTypes)
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
				card = &Card{ID: id}
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
		if len(rows) < observePage || next == c {
			break
		}
		c = next
	}
	out := make([]Card, 0, len(order))
	for _, id := range order {
		if card := byID[id]; !card.Created.IsZero() {
			out = append(out, *card)
		}
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
