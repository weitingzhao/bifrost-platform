package delivery

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
)

// The node metrics build placement reads (decoupling item 5, A5, 2026-11-18;
// they were the server's build_node_metrics.go): what each node's disk
// latency, CPU, disk busy and free memory are, from Prometheus, joined by
// JoinNodeMetrics. The server hands Read the Prometheus reader.

const (
	nodeDisk = `device=~"nvme[0-9]+n[0-9]+|sd[a-z]+"`
	// A seven-day average moves slowly and is the dear query; CI placement asks
	// every minute.
	nodeLatencyKeep = time.Hour
)

func nodeLatencyOver(window string) string {
	return `sum by (instance) (rate(node_disk_write_time_seconds_total{` + nodeDisk + `}[` + window + `])) / sum by (instance) (rate(node_disk_writes_completed_total{` + nodeDisk + `}[` + window + `]))`
}

var nodeLatencyQuery, nodeRecentLatencyQuery = nodeLatencyOver("7d"), nodeLatencyOver("15m")

// PromNodeMetrics is the last answer of nodeLatencyQuery.
type PromNodeMetrics struct {
	mu     sync.Mutex
	at     time.Time
	values map[string]float64
}

// Read reads what build placement weighs, by node address. The write latency is
// the worse of two averages. Seven days: ubt-k3s-05's slow disk reads 6.6 ms while
// it writes little. Fifteen minutes: on 2026-11-18 ubt-k3s-07 answered in 799 ms
// while its 7-day figure still said 15.
func (c *PromNodeMetrics) Read(ctx context.Context, promql func(context.Context, string) ([]telemetry.SamplePoint, error)) (map[string]NodeMetrics, error) {
	read := func(q string) (map[string]float64, error) {
		points, err := promql(ctx, q)
		if err != nil {
			return nil, err
		}
		out := map[string]float64{}
		for _, p := range points {
			address, _, _ := strings.Cut(p.Labels["instance"], ":")
			out[address] = p.Value
		}
		return out, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil || time.Since(c.at) > nodeLatencyKeep {
		latency, err := read(nodeLatencyQuery)
		if err != nil {
			return nil, err
		}
		c.values, c.at = latency, time.Now()
	}
	recent, err := read(nodeRecentLatencyQuery)
	if err != nil {
		return nil, err
	}
	values := []map[string]float64{WorseLatency(c.values, recent), nil, nil, nil}
	for i, q := range []string{
		`1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m]))`,
		`max by (instance) (rate(node_disk_io_time_seconds_total{` + nodeDisk + `}[5m]))`,
		`max by (instance) (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)`,
	} {
		v, err := read(q)
		if err != nil {
			return nil, err
		}
		values[i+1] = v
	}
	written, err := read(`sum by (instance) (increase(node_disk_written_bytes_total{` + nodeDisk + `}[2h]))`)
	if err != nil {
		return nil, err
	}
	out := JoinNodeMetrics(values[0], values[1], values[2], values[3])
	for address, m := range out {
		m.WrittenBytes2h = written[address]
		out[address] = m
	}
	return out, nil
}

// WorseLatency is, for each node of long, the higher of its two averages. A node
// that wrote nothing lately has no recent figure (NaN or none), and keeps long's.
func WorseLatency(long, recent map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(long))
	for address, l := range long {
		if r, ok := recent[address]; ok && r == r && r > l {
			l = r
		}
		out[address] = l
	}
	return out
}
