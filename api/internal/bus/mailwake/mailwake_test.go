package mailwake

import (
	"encoding/json"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

func letter(t *testing.T, id, to string) []byte {
	t.Helper()
	b, err := json.Marshal(relay.Envelope{ID: id, Type: "seat.mail.arrived", Subject: to})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Origin: 2026-11-18, the coordinator: seat mail moves onto the bus (channel
// W01: the inbox's long poll read PostgreSQL every second), and the Bus page's
// "no one listening" must be believable: a receipt only when a waiting seat
// was really woken, not whenever the process saw the event.
func TestALetterWakesItsReceiverOnlyAndIsHeardOnlyIfSomeoneWaits(t *testing.T) {
	var h Hub
	a, stopA := h.Listen("thread:a")
	defer stopA()
	b, stopB := h.Listen("thread:b")
	defer stopB()
	if got := h.Take(letter(t, "ev-1", "thread:a")); got != "ev-1" {
		t.Fatalf("a waiter was woken, the receipt's event = %q", got)
	}
	select {
	case <-a:
	default:
		t.Fatal("the receiver was not woken")
	}
	select {
	case <-b:
		t.Fatal("another seat was woken")
	default:
	}
	if got := h.Take(letter(t, "ev-2", "thread:nobody")); got != "" {
		t.Fatalf("no one waits there, yet a receipt for %q", got)
	}
	if got := h.Take([]byte("not json")); got != "" {
		t.Fatalf("a broken message gave %q", got)
	}
}

// Same origin: mail landing between a waiter's read and its wait must not be
// lost, so one signal stays until taken, and repeats never block the bus's
// callback; a waiter that left is no longer counted.
func TestASignalWaitsForItsWaiterAndNeverBlocks(t *testing.T) {
	var h Hub
	ch, stop := h.Listen("thread:a")
	for i := 0; i < 3; i++ {
		if n := h.Signal("thread:a"); n != 1 {
			t.Fatalf("waiters = %d", n)
		}
	}
	<-ch
	stop()
	if n := h.Signal("thread:a"); n != 0 {
		t.Fatalf("after stop, waiters = %d", n)
	}
}
