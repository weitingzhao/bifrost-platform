package server

import (
	"context"

	busalerts "github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
)

// webhookFailing is what Prometheus says of Alertmanager's webhook to the
// platform; known is false when Prometheus does not answer.
func (s *Server) webhookFailing(ctx context.Context) (failing, known bool) {
	v, err := s.promql(ctx, busalerts.WebhookQuery)
	if err != nil {
		return false, false
	}
	return len(v) > 0, true
}
