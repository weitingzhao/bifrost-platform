package gate

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// WakeOn subscribes, on core NATS (no consumer, nothing kept), to every event
// of the tenant's environment and wakes the gate's waiters on each: one
// subscription per api process.
func WakeOn(nc *nats.Conn, tenant, env string, w *Waker) (*nats.Subscription, error) {
	return nc.Subscribe("ev."+tenant+"."+env+".>", func(*nats.Msg) { w.Notify() })
}

// JSJourney reads an event's journey from the two streams, each from a few
// seconds before the event's ledger time, never from a stream's start.
type JSJourney struct {
	JS          jetstream.JetStream
	Tenant, Env string
}

// The window a journey is looked for in: from Lead before the event's time
// to Window after it, at most MaxScan messages of each stream.
const (
	JourneyLead   = 5 * time.Second
	JourneyWindow = 10 * time.Minute
	MaxScan       = 5000
)

func (j *JSJourney) Published(ctx context.Context, subject, id string, at time.Time) (uint64, time.Time, bool, error) {
	var hit *relay.StreamMsg
	err := relay.ScanFrom(ctx, j.JS, relay.EventStreamName(j.Tenant, j.Env), subject, at.Add(-JourneyLead), at.Add(JourneyWindow), MaxScan,
		func(m relay.StreamMsg) bool {
			if m.Header.Get(jetstream.MsgIDHeader) == id {
				hit = &m
				return true
			}
			return false
		})
	if err != nil || hit == nil {
		return 0, time.Time{}, false, err
	}
	return hit.Seq, hit.At, true, nil
}

func (j *JSJourney) Receipts(ctx context.Context, id string, at time.Time) ([]ReceiptAt, error) {
	var out []ReceiptAt
	err := relay.ScanFrom(ctx, j.JS, relay.ReceiptStreamName(j.Tenant, j.Env), relay.ReceiptSubject(j.Tenant, j.Env),
		at.Add(-JourneyLead), at.Add(JourneyWindow), MaxScan,
		func(m relay.StreamMsg) bool {
			if m.Header.Get(relay.ReceiptEventHeader) != id {
				return false
			}
			r := ReceiptAt{Consumer: m.Header.Get(relay.ReceiptConsumerHeader), At: m.At}
			var body relay.Receipt
			if json.Unmarshal(m.Data, &body) == nil {
				if t, err := time.Parse(time.RFC3339Nano, body.At); err == nil {
					r.At = t
				}
			}
			out = append(out, r)
			return false
		})
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		return nil, nil // no receipt was ever published
	}
	return out, err
}
