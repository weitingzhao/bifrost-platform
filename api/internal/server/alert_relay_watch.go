package server

import (
	"context"
)

// promPresent says whether a Prometheus query has a value; known is false when
// Prometheus does not answer.
func (s *Server) promPresent(ctx context.Context, query string) (present, known bool) {
	v, err := s.promql(ctx, query)
	if err != nil {
		return false, false
	}
	return len(v) > 0, true
}
