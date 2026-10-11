package approvalnotify

import "github.com/weitingzhao/bifrost-platform/api/internal/notice"

// Redact masks common secret shapes. Call it before clipping or storing:
// a tail or a clip that cuts the marker off would otherwise keep the secret.
// The patterns live in notice.
func Redact(s string) string { return notice.Redact(s) }
