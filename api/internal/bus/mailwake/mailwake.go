// Package mailwake wakes the seats waiting on their inbox when the bus says
// mail reached them (seat.mail.arrived; channel W01 of the next generation:
// the inbox's long poll read PostgreSQL every second). The event is only a
// hint (bus/RULES.md E2): a woken waiter reads its inbox, the record, and a
// slow re-read in the waiter covers an event it did not get.
//
// A receipt says someone was there: it is written under Consumer only when
// the event woke a waiter of this process, so the Bus page's "heard" for
// seat.mail.arrived counts letters a waiting seat took at once, and mail
// nobody was waiting for stays unheard until it is read.
package mailwake

import (
	"encoding/json"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// Consumer is the receipts' name, declared in the subscription table's clients.
const Consumer = "inbox-waiters"

// Hub holds the waiters of one process, by the address they wait on. The zero
// value is ready.
type Hub struct {
	mu      sync.Mutex
	waiters map[string]map[chan struct{}]struct{}
}

// Listen registers a waiter on addr. Register before reading the inbox, so
// mail that lands between the read and the wait still wakes it. stop removes
// the waiter.
func (h *Hub) Listen(addr string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.waiters == nil {
		h.waiters = map[string]map[chan struct{}]struct{}{}
	}
	if h.waiters[addr] == nil {
		h.waiters[addr] = map[chan struct{}]struct{}{}
	}
	h.waiters[addr][ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.waiters[addr], ch)
		if len(h.waiters[addr]) == 0 {
			delete(h.waiters, addr)
		}
	}
}

// Signal wakes every waiter on addr and says how many there were. It never
// blocks: a waiter already signalled will read the inbox anyway.
func (h *Hub) Signal(addr string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.waiters[addr] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return len(h.waiters[addr])
}

// Take handles one seat.mail.arrived message off the bus: its subject is the
// receiver. It returns the event's id when a waiter was woken, "" otherwise
// (nobody here was waiting, or the message is not an event).
func (h *Hub) Take(data []byte) string {
	var e relay.Envelope
	if json.Unmarshal(data, &e) != nil || e.Subject == "" || e.ID == "" {
		return ""
	}
	if h.Signal(e.Subject) == 0 {
		return ""
	}
	return e.ID
}
