package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

const nodeDisk = `device=~"nvme[0-9]+n[0-9]+|sd[a-z]+"`

// buildNodeMetrics reads what build placement weighs, by node address. The
// write latency is a 7-day average: ubt-k3s-05's slow disk reads 6.6 ms while
// it writes little, so a short window would rank it level with a fast one.
func (s *Server) buildNodeMetrics(ctx context.Context) (map[string]delivery.NodeMetrics, error) {
	if s.telemetry == nil {
		return nil, fmt.Errorf("no Prometheus reader on this platform")
	}
	values := make([]map[string]float64, 4)
	for i, q := range []string{
		`sum by (instance) (rate(node_disk_write_time_seconds_total{` + nodeDisk + `}[7d])) / sum by (instance) (rate(node_disk_writes_completed_total{` + nodeDisk + `}[7d]))`,
		`1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m]))`,
		`max by (instance) (rate(node_disk_io_time_seconds_total{` + nodeDisk + `}[5m]))`,
		`max by (instance) (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)`,
	} {
		points, err := s.telemetry.PromQL(ctx, q)
		if err != nil {
			return nil, err
		}
		values[i] = map[string]float64{}
		for _, p := range points {
			address, _, _ := strings.Cut(p.Labels["instance"], ":")
			values[i][address] = p.Value
		}
	}
	return delivery.JoinNodeMetrics(values[0], values[1], values[2], values[3]), nil
}
