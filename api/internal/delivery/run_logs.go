package delivery

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
	"time"
)

func splitK8sLogTimestamp(line string) (time.Time, string, bool) {
	return telemetry.SplitK8sLogTimestamp(line)
}
func stripK8sLogTimestamps(data []byte) (string, *time.Time) {
	return telemetry.StripK8sLogTimestamps(data)
}
func maxTimePtr(a, b *time.Time) *time.Time { return telemetry.MaxTimePtr(a, b) }
