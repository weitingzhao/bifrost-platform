package relay

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The bus's two streams measured, not only bounded (LAW-12). Their limits are set in EnsureEventStream and
// EnsureReceiptStream; this says how full they are.

var streamSizes = struct {
	sync.Mutex
	bytes, msgs map[string]uint64
}{bytes: map[string]uint64{}, msgs: map[string]uint64{}}

// The backlog of each durable consumer of the events stream (2026-11-21, the
// Bus page's own list of what it does not measure: "backlog per
// subscription"): pending is what the stream holds for it and it has not
// been handed yet, ack_pending what it was handed and has not acknowledged.
// A reader that stopped shows as a pending that only grows.
var consumerBacklog = struct {
	sync.Mutex
	pending, ackPending map[string]uint64
}{pending: map[string]uint64{}, ackPending: map[string]uint64{}}

func recordConsumerBacklogs(pending, ackPending map[string]uint64) {
	consumerBacklog.Lock()
	defer consumerBacklog.Unlock()
	consumerBacklog.pending, consumerBacklog.ackPending = pending, ackPending
}

func recordStreamSize(name string, bytes, msgs uint64) {
	streamSizes.Lock()
	defer streamSizes.Unlock()
	streamSizes.bytes[name], streamSizes.msgs[name] = bytes, msgs
}

// WatchStreamSizes reads the two streams' size, and the events stream's
// durable consumers' backlog, every minute until ctx ends.
func WatchStreamSizes(ctx context.Context, js jetstream.JetStream, tenant, env string) {
	names := []string{EventStreamName(tenant, env), ReceiptStreamName(tenant, env)}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		for _, name := range names {
			ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if s, err := js.Stream(ictx, name); err == nil {
				if info, err := s.Info(ictx); err == nil {
					recordStreamSize(name, info.State.Bytes, info.State.Msgs)
				}
			}
			cancel()
		}
		readConsumerBacklogs(ctx, js, names[0])
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func readConsumerBacklogs(ctx context.Context, js jetstream.JetStream, stream string) {
	ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := js.Stream(ictx, stream)
	if err != nil {
		return
	}
	pending, ackPending := map[string]uint64{}, map[string]uint64{}
	l := s.ListConsumers(ictx)
	for info := range l.Info() {
		if info == nil || info.Config.Durable == "" {
			continue
		}
		pending[info.Name] = info.NumPending
		ackPending[info.Name] = uint64(info.NumAckPending)
	}
	if l.Err() != nil {
		return
	}
	recordConsumerBacklogs(pending, ackPending)
}

func writeConsumerBacklogs(b *strings.Builder) {
	consumerBacklog.Lock()
	defer consumerBacklog.Unlock()
	if len(consumerBacklog.pending) == 0 {
		return
	}
	names := make([]string, 0, len(consumerBacklog.pending))
	for n := range consumerBacklog.pending {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(b, "# HELP bifrost_bus_consumer_pending Events the stream holds for the durable consumer and has not handed it yet\n# TYPE bifrost_bus_consumer_pending gauge\n")
	for _, n := range names {
		fmt.Fprintf(b, "bifrost_bus_consumer_pending{consumer=%q} %d\n", n, consumerBacklog.pending[n])
	}
	fmt.Fprintf(b, "# HELP bifrost_bus_consumer_ack_pending Events handed to the durable consumer and not acknowledged\n# TYPE bifrost_bus_consumer_ack_pending gauge\n")
	for _, n := range names {
		fmt.Fprintf(b, "bifrost_bus_consumer_ack_pending{consumer=%q} %d\n", n, consumerBacklog.ackPending[n])
	}
}

func writeStreamSizes(b *strings.Builder) {
	streamSizes.Lock()
	defer streamSizes.Unlock()
	if len(streamSizes.bytes) == 0 {
		return
	}
	names := make([]string, 0, len(streamSizes.bytes))
	for n := range streamSizes.bytes {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(b, "# HELP bifrost_bus_stream_bytes Bytes the stream holds now\n# TYPE bifrost_bus_stream_bytes gauge\n")
	for _, n := range names {
		fmt.Fprintf(b, "bifrost_bus_stream_bytes{stream=%q} %d\n", n, streamSizes.bytes[n])
	}
	fmt.Fprintf(b, "# HELP bifrost_bus_stream_messages Messages the stream holds now\n# TYPE bifrost_bus_stream_messages gauge\n")
	for _, n := range names {
		fmt.Fprintf(b, "bifrost_bus_stream_messages{stream=%q} %d\n", n, streamSizes.msgs[n])
	}
}
