package approvals

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

// renew past grace notifies once. A later uncertain finish updates the error
// and does not send approval.unknown again.
func TestRenewPastGraceThenUncertainFinishNotifiesOnce(t *testing.T) {
	f := relay(t)
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return base })
	rec, lease := claimed(t, svc)
	svc.SetClock(func() time.Time { return base.Add(leaseFor + unknownGrace + time.Second) })
	out, renewed, err := svc.renew(rec.ID, lease)
	if err != nil || renewed || out.Status != StatusUnknown {
		t.Fatalf("renew status=%s renewed=%v err=%v", out.Status, renewed, err)
	}
	_ = deliveriesOf(t, svc, rec.ID)

	_, evs, err := svc.finishPlatform(rec.ID, lease, base, nil, actions.Uncertain("create outcome unknown; outcome needs checking"))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("already-unknown finish emitted %+v", evs)
	}
	svc.record(nil, evs)
	got, ok := svc.find(rec.ID)
	if !ok || !strings.Contains(got.Error, "create outcome unknown") {
		t.Fatalf("stored error = %q", got.Error)
	}
	ds := deliveriesOf(t, svc, rec.ID)
	pushes := f.got()
	if len(pushes) != 1 {
		t.Fatalf("pushes = %+v", pushes)
	}
	if !strings.Contains(pushes[0].Message, "lease lapsed") {
		t.Fatalf("first push hid the lease reason: %q", pushes[0].Message)
	}
	n := 0
	for _, d := range ds {
		if d.Kind == "unknown" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("unknown deliveries = %d (%+v)", n, ds)
	}
}
