package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// The bus is off unless BUS_NATS_URL is set (step 1 of the communication
// layer: nothing changes for an environment that does not set it).
const (
	EnvURL            = "BUS_NATS_URL"
	EnvRelayPassword  = "BUS_NATS_RELAY_PASSWORD"
	EnvReaderPassword = "BUS_NATS_READER_PASSWORD"

	// The two static users of step 1 (RULES.md §11): the relay publishes,
	// the reader only subscribes.
	RelayUser  = "relay"
	ReaderUser = "reader"
)

// Config is where the bus is and who this process is on it.
type Config struct {
	URL            string
	Env            string
	RelayPassword  string
	ReaderPassword string
}

// ConfigFromEnv reads the bus's settings for an environment. ok is false
// when BUS_NATS_URL is not set (the bus is off); an error says why a set URL
// cannot be used.
func ConfigFromEnv(env string) (Config, bool, error) {
	c := Config{
		URL:            strings.TrimSpace(os.Getenv(EnvURL)),
		Env:            strings.TrimSpace(env),
		RelayPassword:  os.Getenv(EnvRelayPassword),
		ReaderPassword: os.Getenv(EnvReaderPassword),
	}
	if c.URL == "" {
		return c, false, nil
	}
	if !EnvRE.MatchString(c.Env) {
		return c, false, fmt.Errorf("bus: %s is set but the environment %q is not a subject segment (AGENTRT_ENV)", EnvURL, c.Env)
	}
	return c, true, nil
}

// Connect opens a connection that keeps reconnecting: a NATS that is down at
// start or later is waited for, not fatal.
func Connect(url, user, password, name string) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name(name),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.RetryOnFailedConnect(true),
	}
	if user != "" || password != "" {
		opts = append(opts, nats.UserInfo(user, password))
	}
	return nats.Connect(url, opts...)
}

// EnsureEventStream makes the events stream of a tenant's environment as
// RULES.md §11 sets it, or brings an existing one to that configuration.
func EnsureEventStream(ctx context.Context, js jetstream.JetStream, tenant, env string) (jetstream.Stream, error) {
	return js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      EventStreamName(tenant, env),
		Subjects:  []string{"ev." + tenant + "." + env + ".>"},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    30 * 24 * time.Hour,
		// The ROCKET account holds 10 GB of file store for every stream of both
		// environments (k8s/bus/nats-config.yaml): EV 2 GiB + RCPT 512 MiB each,
		// 5 GiB in all. At 5 GiB each the two EV streams took it all and the
		// first receipt stream was refused (PROD, 2026-11-16).
		MaxBytes:   EventStreamBytes,
		Discard:    jetstream.DiscardOld,
		Duplicates: 2 * time.Minute,
	})
}

// EnsureReceiptStream makes the receipts stream if it is not there. Whoever
// publishes a receipt first makes it; an existing one is left as it is.
func EnsureReceiptStream(ctx context.Context, js jetstream.JetStream, tenant, env string) (jetstream.Stream, error) {
	name := ReceiptStreamName(tenant, env)
	s, err := js.Stream(ctx, name)
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return nil, err
	}
	s, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:      name,
		Subjects:  []string{ReceiptSubject(tenant, env)},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    7 * 24 * time.Hour,
		MaxBytes:  ReceiptStreamBytes,
		Discard:   jetstream.DiscardOld,
	})
	if errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
		return js.Stream(ctx, name)
	}
	return s, err
}

// Out is where the relay publishes: the events stream.
type Out interface {
	// LastCursor is the cursor the stream's last message carries; ok is
	// false for an empty stream.
	LastCursor(ctx context.Context) (Cursor, bool, error)
	// Publish publishes one envelope and returns once the stream has it
	// (PubAck), deduplicated by id.
	Publish(ctx context.Context, subject, id string, cursor Cursor, data []byte) error
}

// JSOut is Out on a JetStream stream.
type JSOut struct {
	JS     jetstream.JetStream
	Stream string
}

func (o *JSOut) LastCursor(ctx context.Context) (Cursor, bool, error) {
	s, err := o.JS.Stream(ctx, o.Stream)
	if err != nil {
		return Cursor{}, false, err
	}
	info, err := s.Info(ctx)
	if err != nil {
		return Cursor{}, false, err
	}
	if info.State.Msgs == 0 {
		return Cursor{}, false, nil
	}
	msg, err := s.GetMsg(ctx, info.State.LastSeq)
	if err != nil {
		return Cursor{}, false, err
	}
	c, ok := ParseCursor(msg.Header.Get(CursorHeader))
	if !ok {
		return Cursor{}, false, fmt.Errorf("bus: the last message of %s (seq %d) has no %s header", o.Stream, msg.Sequence, CursorHeader)
	}
	return c, true, nil
}

func (o *JSOut) Publish(ctx context.Context, subject, id string, cursor Cursor, data []byte) error {
	m := nats.NewMsg(subject)
	m.Header.Set(CursorHeader, cursor.String())
	m.Header.Set("Content-Type", "application/cloudevents+json")
	m.Data = data
	_, err := o.JS.PublishMsg(ctx, m, jetstream.WithMsgID(id))
	return err
}

// Receipts publishes delivery receipts (rcpt.<tenant>.<env>, the event id and
// the consumer in headers), making their stream on first use.
type Receipts struct {
	JS          jetstream.JetStream
	Tenant, Env string
	mu          sync.Mutex
	ensured     bool
}

// Publish records that consumer was handed the event id.
func (r *Receipts) Publish(ctx context.Context, id, consumer string) error {
	r.mu.Lock()
	if !r.ensured {
		if _, err := EnsureReceiptStream(ctx, r.JS, r.Tenant, r.Env); err != nil {
			r.mu.Unlock()
			return err
		}
		r.ensured = true
	}
	r.mu.Unlock()
	body, err := json.Marshal(Receipt{Consumer: consumer, At: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	m := nats.NewMsg(ReceiptSubject(r.Tenant, r.Env))
	m.Header.Set(ReceiptEventHeader, id)
	m.Header.Set(ReceiptConsumerHeader, consumer)
	m.Data = body
	_, err = r.JS.PublishMsg(ctx, m)
	return err
}

// ConsumeDurable runs handle for each event of one type on a durable
// consumer of the events stream (RULES.md §8 item 1: only resident services
// hold one). A new consumer starts at new events, not the stream's history.
// handle's nil acknowledges; an error asks for the event again later.
func ConsumeDurable(ctx context.Context, js jetstream.JetStream, tenant, env, durable, typ string,
	handle func(Envelope) error) (jetstream.ConsumeContext, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, EventStreamName(tenant, env), jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: EventSubject(tenant, env, typ),
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
		AckWait:       time.Minute,
		MaxDeliver:    20,
	})
	if err != nil {
		return nil, err
	}
	return cons.Consume(func(msg jetstream.Msg) {
		var e Envelope
		if err := json.Unmarshal(msg.Data(), &e); err != nil {
			// Not an envelope: nothing will make it one.
			_ = msg.Term()
			return
		}
		if err := handle(e); err != nil {
			_ = msg.NakWithDelay(10 * time.Second)
			return
		}
		_ = msg.Ack()
	})
}

// StreamMsg is one message read back from a stream.
type StreamMsg struct {
	Seq    uint64
	At     time.Time
	Header nats.Header
	Data   []byte
}

// ScanFrom reads, oldest first, the stream's messages on subject stored at or
// after from, on an ordered (ephemeral) consumer: it stops when stop says
// so, at the first message stored after until, after max messages, or when
// none is left.
func ScanFrom(ctx context.Context, js jetstream.JetStream, stream, subject string, from, until time.Time, max int,
	stop func(StreamMsg) bool) error {
	cons, err := js.OrderedConsumer(ctx, stream, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{subject},
		DeliverPolicy:  jetstream.DeliverByStartTimePolicy,
		OptStartTime:   &from,
	})
	if err != nil {
		return err
	}
	for read := 0; read < max; {
		batch, err := cons.Fetch(min(100, max-read), jetstream.FetchMaxWait(time.Second))
		if err != nil {
			return err
		}
		n, pending := 0, uint64(0)
		for m := range batch.Messages() {
			n++
			read++
			md, err := m.Metadata()
			if err != nil {
				return err
			}
			pending = md.NumPending
			if md.Timestamp.After(until) {
				return nil
			}
			if stop(StreamMsg{Seq: md.Sequence.Stream, At: md.Timestamp, Header: m.Headers(), Data: m.Data()}) {
				return nil
			}
		}
		if err := batch.Error(); err != nil && !errors.Is(err, nats.ErrTimeout) && !errors.Is(err, jetstream.ErrNoMessages) {
			return err
		}
		if n == 0 || pending == 0 {
			return nil
		}
	}
	return nil
}

// Stream sizes. Both environments' streams live in the one ROCKET account,
// whose file store is 10 GB (bifrost-trade-infra k8s/bus/nats-config.yaml).
const (
	EventStreamBytes   = 2 << 30
	ReceiptStreamBytes = 512 << 20
	accountFileBytes   = 10_000_000_000
)
