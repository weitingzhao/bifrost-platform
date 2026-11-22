package evalreadings

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 2026-11-22, Audit asked for the map's domain page reads as a daily archived
// number (it had only ad-hoc reads). The increase is rounded, the query is at
// the window's end, and no series is unmeasured, not 0.
func TestCatalogDomainReads(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC), 1)
	var asked string
	prom := func(_ context.Context, expr string) ([]Sample, error) {
		asked = expr
		return []Sample{{Labels: map[string]string{}, Value: 39.6}}, nil
	}
	rs := catalogDomainReadings(context.Background(), prom, w)
	if len(rs) != 1 || rs[0].Status != StatusOK || *rs[0].Value != 40 {
		t.Fatalf("reading: %+v", rs)
	}
	if !strings.Contains(asked, `handler="/api/v1/catalog/domain"`) || !strings.Contains(asked, "@ "+itoa(w.To.Unix())) {
		t.Fatalf("query: %s", asked)
	}
	none := func(context.Context, string) ([]Sample, error) { return nil, nil }
	if rs := catalogDomainReadings(context.Background(), none, w); rs[0].Status != StatusUnmeasured {
		t.Fatalf("no series must be unmeasured: %+v", rs[0])
	}
}
