package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// WindowSender has the shape of threadinbox.Sender: the platform posts one
// message per key; a repeated key is a successful no-op.
type WindowSender func(ctx context.Context, key, to, body string) error

// windowNotifier sends queue notices (design §4). Only entries with a thread
// get messages. Keys already sent are skipped here; the sender's store is the
// durable dedup across processes. A failed send is retried on the next tick.
type windowNotifier struct {
	mu      sync.Mutex
	send    WindowSender
	sent    map[string]time.Time
	pending []windowNotice
}

// SetWindowNotifier installs the runtime sender. Without one the queue still
// works, only without messages.
func (h *Handler) SetWindowNotifier(send WindowSender) {
	h.notices.mu.Lock()
	defer h.notices.mu.Unlock()
	h.notices.send = send
}

func (n *windowNotifier) deliver(ctx context.Context, ns string, notices []windowNotice, now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.send == nil {
		return
	}
	if n.sent == nil {
		n.sent = map[string]time.Time{}
	}
	for k, at := range n.sent {
		if now.Sub(at) > 24*time.Hour {
			delete(n.sent, k)
		}
	}
	all := append(n.pending, notices...)
	n.pending = nil
	for _, m := range all {
		if m.To == "" {
			continue
		}
		if _, done := n.sent[m.Key]; done {
			continue
		}
		if err := n.send(ctx, m.Key, m.To, windowEnvelope(ns, m)); err != nil {
			slog.Warn("release window notice failed; retrying next tick", "key", m.Key, "err", err)
			n.pending = append(n.pending, m)
			continue
		}
		n.sent[m.Key] = now
	}
}

// windowEnvelope is the message envelope of mcp/platform/src/messageEnvelope.ts
// with the platform's own type "fact".
func windowEnvelope(ns string, m windowNotice) string {
	e := m.Entry
	var summary, text string
	switch m.Topic {
	case "window.your_turn":
		summary = fmt.Sprintf("Release window reserved for %s (%s); take it with PUT within the reservation", e.Who, e.What)
	case "window.missed":
		summary = fmt.Sprintf("Release window reservation for %s expired unused; skipped to the next in queue", e.Who)
	case "window.queue_timeout":
		summary = fmt.Sprintf("Queue entry for %s timed out at %s; queue again if still needed", e.Who, e.Until)
	case "window.still_held":
		summary = fmt.Sprintf("Release window still held by %s; you are first in queue", m.Holder.Who)
	}
	if m.Holder.Who != "" {
		text = fmt.Sprintf("Holder: %s (what=%s) until %s.", m.Holder.Who, m.Holder.What, m.Holder.ExpiresAt)
	}
	if e.Resume != "" {
		text += " Resume: " + e.Resume
	}
	body := map[string]any{"v": 1, "type": "fact", "summary": summary,
		"about": []string{"topic:" + m.Topic, "what:" + e.What},
		"refs":  []string{"release-window:" + ns}}
	if text != "" {
		body["text"] = text
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// advanceWindowQueueOnce runs one advancer step and sends its notices.
func (h *Handler) advanceWindowQueueOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cs, _, err := h.svc.cluster.KubernetesClient()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	notices, err := advanceWindowQueue(ctx, cs, h.svc.PipelinesNamespace(), now, loadWindowQueueConfig())
	if apierrors.IsForbidden(err) {
		return nil // a read-only identity leaves the queue to the writer
	}
	h.notices.deliver(ctx, h.svc.PipelinesNamespace(), notices, now)
	return err
}
