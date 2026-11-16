package delivery

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
	"time"
)

func stripK8sLogTimestamps(data []byte) (string, *time.Time) {
	return telemetry.StripK8sLogTimestamps(data)
}
func maxTimePtr(a, b *time.Time) *time.Time { return telemetry.MaxTimePtr(a, b) }
