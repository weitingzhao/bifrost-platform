package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/drill"
)

// startBusDrill has the workers drill the problem channels that never fire in
// normal life (bus/drill): one event a day per channel, on its own stream that
// the bus's readers leave out.
func (s *Server) startBusDrill(ctx context.Context) { drill.Run(ctx, s.bus.db, busLogf) }
