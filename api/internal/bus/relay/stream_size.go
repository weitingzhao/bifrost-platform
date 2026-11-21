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

// The bus's two streams measured, not only bounded (2026-11-19, the Owner: data
// that grows keeps a fixed size). Their limits are set in EnsureEventStream and
// EnsureReceiptStream; this says how full they are.

var streamSizes = struct {
	sync.Mutex
	bytes, msgs map[string]uint64
}{bytes: map[string]uint64{}, msgs: map[string]uint64{}}

func recordStreamSize(name string, bytes, msgs uint64) {
	streamSizes.Lock()
	defer streamSizes.Unlock()
	streamSizes.bytes[name], streamSizes.msgs[name] = bytes, msgs
}

// WatchStreamSizes reads the two streams' size every minute until ctx ends.
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
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
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
