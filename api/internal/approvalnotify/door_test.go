package approvalnotify

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// TestServerWrittenNoticesPassTheDoor checks every approval kind, plus the
// release-policy sentence the checker sends. A tab in one of those would be
// fixed in the sentence, not by weakening the door.
func TestServerWrittenNoticesPassTheDoor(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	it := gitopsSync()
	it.Reason = "Publish the candidate."
	it.Error = "invalid params"
	now := time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)
	for _, kind := range []string{KindCreated, KindFailed, KindUnknown, KindNotExecuted, KindWaiting, KindExpiring} {
		m := Compose(kind, it, now)
		if strings.ContainsRune(m.Title, '\t') || strings.ContainsRune(m.Message, '\t') || notice.HasHidden(m.Title) || notice.HasHidden(m.Message) {
			t.Fatalf("%s produced unclean text\ntitle %q\nbody %q", kind, m.Title, m.Message)
		}
		ds, err := notice.Send(context.Background(), m)
		if err != nil || len(ds) != 1 || ds[0].Result != notice.ResultSkipped {
			t.Fatalf("%s send err %v deliveries %+v", kind, err, ds)
		}
	}
	msg := notice.Message{
		Title:   "Release policy expires in 23h",
		Message: "Policy pol_1 expires at 2026-10-12T15:04:00Z. Releases will wait for manual approval after that. Sign a new one: bifrost-trade-infra/scripts/release/release.sh policy sign",
	}
	ds, err := notice.Send(context.Background(), msg)
	if err != nil || ds[0].Result != notice.ResultSkipped {
		t.Fatalf("release policy send err %v deliveries %+v", err, ds)
	}
}
