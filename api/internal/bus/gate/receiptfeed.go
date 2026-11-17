package gate

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// ReceiptNote is one receipt as the stream passes it on: which event, who
// took it, when.
type ReceiptNote struct {
	ID       string    `json:"id"`
	Consumer string    `json:"consumer"`
	At       time.Time `json:"at"`
}

// ReceiptFeed hands the receipts the gate follows to the streams that asked
// for them (/bus/stream?receipts=1, the Bus page's nerve map: a pulse lands
// when its receipt comes). One follower per process, any number of
// listeners; a listener that falls behind loses receipts, never blocks the
// others.
type ReceiptFeed struct {
	mu   sync.Mutex
	subs map[chan ReceiptNote]struct{}
}

// ListenerBuffer is how many receipts a listener may be behind.
const ListenerBuffer = 256

// Listen is a new listener and the function that ends it.
func (f *ReceiptFeed) Listen() (<-chan ReceiptNote, func()) {
	ch := make(chan ReceiptNote, ListenerBuffer)
	f.mu.Lock()
	if f.subs == nil {
		f.subs = map[chan ReceiptNote]struct{}{}
	}
	f.subs[ch] = struct{}{}
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.subs, ch)
		f.mu.Unlock()
	}
}

// Note passes a receipt to every listener.
func (f *ReceiptFeed) Note(n ReceiptNote) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subs {
		select {
		case ch <- n:
		default: // behind: this one is lost for that listener
		}
	}
}

// FollowReceipts follows the tenant's receipt stream from now with an
// ordered consumer (the reader user may create one; it may not subscribe to
// rcpt.> directly) and notes each receipt, until ctx ends. A lost consumer
// is made again after a pause.
func FollowReceipts(ctx context.Context, js jetstream.JetStream, tenant, env string, f *ReceiptFeed, logf func(string, ...any)) {
	stream := relay.ReceiptStreamName(tenant, env)
	for ctx.Err() == nil {
		err := followOnce(ctx, js, stream, relay.ReceiptSubject(tenant, env), f)
		if ctx.Err() != nil {
			return
		}
		if logf != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
			logf("bus gate: following receipts: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func followOnce(ctx context.Context, js jetstream.JetStream, stream, subject string, f *ReceiptFeed) error {
	cons, err := js.OrderedConsumer(ctx, stream, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{subject},
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		batch, err := cons.Fetch(100, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			return err
		}
		for m := range batch.Messages() {
			n := ReceiptNote{ID: m.Headers().Get(relay.ReceiptEventHeader), Consumer: m.Headers().Get(relay.ReceiptConsumerHeader)}
			var body relay.Receipt
			if json.Unmarshal(m.Data(), &body) == nil {
				n.At, _ = time.Parse(time.RFC3339Nano, body.At)
			}
			if n.At.IsZero() {
				if md, err := m.Metadata(); err == nil {
					n.At = md.Timestamp
				}
			}
			if n.ID != "" {
				f.Note(n)
			}
		}
		if err := batch.Error(); err != nil && !errors.Is(err, nats.ErrTimeout) && !errors.Is(err, jetstream.ErrNoMessages) {
			return err
		}
	}
	return ctx.Err()
}
