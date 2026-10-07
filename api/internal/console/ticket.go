package console

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// A browser WebSocket cannot send an Authorization header, so the shell is
// opened in two steps: an operator POSTs /console/ws-ticket for one host and
// gets a ticket that is good for one connection to that host within ticketTTL.
// Until 2026-10-07 /console/ws took no credential at all (TD-203).
const ticketTTL = 30 * time.Second

type ticket struct {
	node      string
	principal string
	expires   time.Time
}

type ticketStore struct {
	mu      sync.Mutex
	tickets map[string]ticket
	now     func() time.Time
}

func newTicketStore() *ticketStore {
	return &ticketStore{tickets: map[string]ticket{}, now: time.Now}
}

func (s *ticketStore) issue(node, principal string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, t := range s.tickets {
		if now.After(t.expires) {
			delete(s.tickets, k)
		}
	}
	s.tickets[id] = ticket{node: node, principal: principal, expires: now.Add(ticketTTL)}
	return id, nil
}

// consume returns the ticket's principal when id is unexpired and was issued
// for node; a ticket is spent by its first use, valid or not.
func (s *ticketStore) consume(id, node string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tickets[id]
	if !ok {
		return "", false
	}
	delete(s.tickets, id)
	if s.now().After(t.expires) || t.node != node {
		return "", false
	}
	return t.principal, true
}

// HandleTicket serves POST /console/ws-ticket?node=<id> (operator).
func (h *Handler) HandleTicket(w http.ResponseWriter, r *http.Request) {
	node := strings.TrimSpace(r.URL.Query().Get("node"))
	if node == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
		return
	}
	if _, ok := FindHostInList(h.resolveHosts(r.Context()), node, ""); !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "host not in allowlist"})
		return
	}
	id, err := h.tickets.issue(node, actuation.PrincipalFromContext(r.Context()).Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ticket: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": id, "expires_in_s": int(ticketTTL / time.Second)})
}
