package gate

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// The roster: the bus recognises users coming and going (ISSUE-34). Who uses the bus is read from what it already has: the gate's own
// requests (each reader names itself with client=), the receipt stream
// (readers and durable consumers leave one per event they get) and the
// subscription table's declared clients. Nothing reports in separately.

// Statuses of a roster entry.
const (
	RosterActive = "active" // declared, came within lost_after
	RosterLost   = "lost"   // declared, has not come for longer than lost_after
	RosterNew    = "new"    // came, not declared: shown, not refused
	RosterIdle   = "idle"   // declared with no lost_after (a dispatcher row): quiet is normal
)

// RosterEntry is one user.
type RosterEntry struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind,omitempty"`
	Declared  bool      `json:"declared"`
	LostAfter string    `json:"lost_after,omitempty"`
	LastSeen  time.Time `json:"last_seen,omitzero"`
	Receipts  int       `json:"receipts"`
	Status    string    `json:"status"`
	Note      string    `json:"note,omitempty"`
}

// seenClients is the last request of each reader, by receipt consumer name,
// in this api process (lost on restart; the receipts carry the history).
type seenClients struct {
	mu sync.Mutex
	at map[string]time.Time
}

func (s *seenClients) mark(name string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.at == nil {
		s.at = map[string]time.Time{}
	}
	s.at[name] = at
}

func (s *seenClients) snapshot() map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]time.Time, len(s.at))
	for k, v := range s.at {
		out[k] = v
	}
	return out
}

// ReceiptCount is what the receipts say of one consumer.
type ReceiptCount struct {
	Last  time.Time
	Count int
}

// RosterSource reads the receipts since a time.
type RosterSource interface {
	ReceiptsSince(ctx context.Context, since time.Time, max int) (map[string]ReceiptCount, error)
}

// ReceiptsSince groups the receipt stream's messages since since by consumer.
func (j *JSJourney) ReceiptsSince(ctx context.Context, since time.Time, max int) (map[string]ReceiptCount, error) {
	rows, _, err := j.recentReceipts(ctx, since, max)
	if err != nil {
		return nil, err
	}
	out := map[string]ReceiptCount{}
	for _, m := range rows {
		r := out[m.consumer]
		r.Count++
		if m.at.After(r.Last) {
			r.Last = m.at
		}
		out[m.consumer] = r
	}
	return out, nil
}

// Roster builds the list: declared clients first (active, lost or idle), then
// the names that came without being declared (new). An undeclared name last
// seen before since (when this process loaded its tables) is a user that
// left with an earlier table: it is not listed (2026-11-16, the tenant
// drill: demo's names stayed on the page as new after it was removed).
func Roster(declared []registry.Client, polled map[string]time.Time, receipts map[string]ReceiptCount, now, since time.Time) []RosterEntry {
	var out []RosterEntry
	known := map[string]bool{}
	last := func(name string) (time.Time, int) {
		t := polled[name]
		r := receipts[name]
		if r.Last.After(t) {
			t = r.Last
		}
		return t, r.Count
	}
	for _, c := range declared {
		known[c.Name] = true
		e := RosterEntry{Name: c.Name, Kind: c.Kind, Declared: true, LostAfter: c.LostAfter, Note: c.Note}
		e.LastSeen, e.Receipts = last(c.Name)
		d, err := time.ParseDuration(c.LostAfter)
		switch {
		case c.LostAfter == "" || err != nil:
			e.Status = RosterIdle
			if !e.LastSeen.IsZero() {
				e.Status = RosterActive
			}
		case e.LastSeen.IsZero() || now.Sub(e.LastSeen) > d:
			e.Status = RosterLost
		default:
			e.Status = RosterActive
		}
		out = append(out, e)
	}
	var fresh []string
	for name := range polled {
		if !known[name] {
			fresh = append(fresh, name)
			known[name] = true
		}
	}
	for name := range receipts {
		if !known[name] {
			fresh = append(fresh, name)
			known[name] = true
		}
	}
	sort.Strings(fresh)
	for _, name := range fresh {
		e := RosterEntry{Name: name, Status: RosterNew}
		e.LastSeen, e.Receipts = last(name)
		if e.LastSeen.Before(since) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// HandleClients is GET /api/v1/bus/clients?hours=24: every user of the bus
// with its status, last time it came and receipts in the window (1 to 168
// hours of the receipt stream, at most 20000 receipts). Read-only.
func (g *Gate) HandleClients(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if s := r.URL.Query().Get("hours"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 168 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hours is 1 to 168"})
			return
		}
		hours = n
	}
	now := time.Now()
	receipts := map[string]ReceiptCount{}
	var readErr string
	if src, ok := g.Journey.(RosterSource); ok && src != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		got, err := src.ReceiptsSince(ctx, now.Add(-time.Duration(hours)*time.Hour), 20000)
		if err != nil {
			readErr = err.Error()
		} else {
			receipts = got
		}
	}
	body := map[string]any{"tenant": g.Tenant.Name, "env": g.Env, "hours": hours,
		"clients": Roster(g.Clients, g.seen.snapshot(), receipts, now, g.TablesLoadedAt)}
	if readErr != "" {
		body["receipts_error"] = readErr
	}
	writeJSON(w, http.StatusOK, body)
}
