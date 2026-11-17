package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/observe"
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
	cards, err := observeCards(r.Context(), &relay.PGLedger{DB: func() relay.TxBeginner { return pool }, EventsAfter: busEventsAfter}, busCursorAt(pool), since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, observe.Build(now, hours, letters, status, cards))
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
