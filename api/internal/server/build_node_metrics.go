package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

const (
	nodeDisk = `device=~"nvme[0-9]+n[0-9]+|sd[a-z]+"`
	// A seven-day average moves slowly and is the dear query; CI placement asks
	// every minute.
	nodeLatencyKeep = time.Hour
)

var nodeLatencyQuery = `sum by (instance) (rate(node_disk_write_time_seconds_total{` + nodeDisk + `}[7d])) / sum by (instance) (rate(node_disk_writes_completed_total{` + nodeDisk + `}[7d]))`

// nodeLatencyCache is the last answer of nodeLatencyQuery.
type nodeLatencyCache struct {
	mu     sync.Mutex
	at     time.Time
	values map[string]float64
}

// buildNodeMetrics reads what build placement weighs, by node address. The
// write latency is a 7-day average: ubt-k3s-05's slow disk reads 6.6 ms while
// it writes little, so a short window would rank it level with a fast one.
func (s *Server) buildNodeMetrics(ctx context.Context) (map[string]delivery.NodeMetrics, error) {
	if s.telemetry == nil {
		return nil, fmt.Errorf("no Prometheus reader on this platform")
	}
	read := func(q string) (map[string]float64, error) {
		points, err := s.telemetry.PromQL(ctx, q)
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
	s.nodeLatency.mu.Lock()
	defer s.nodeLatency.mu.Unlock()
	if s.nodeLatency.values == nil || time.Since(s.nodeLatency.at) > nodeLatencyKeep {
		latency, err := read(nodeLatencyQuery)
		if err != nil {
			return nil, err
		}
		s.nodeLatency.values, s.nodeLatency.at = latency, time.Now()
	}
	values := []map[string]float64{s.nodeLatency.values, nil, nil, nil}
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
	return delivery.JoinNodeMetrics(values[0], values[1], values[2], values[3]), nil
}
