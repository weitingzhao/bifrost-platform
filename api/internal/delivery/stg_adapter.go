package delivery

import (
	"context"
	"time"
)

// stgContext bounds reads of pipeline and task runs.
func stgContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 10*time.Second)
}
