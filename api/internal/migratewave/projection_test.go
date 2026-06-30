package migratewave

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

func TestProjectWaveStatus(t *testing.T) {
	cases := []struct {
		idx, done, ready int
		status           string
		want             string
	}{
		{0, 3, 0, "in_progress", "done"},
		{3, 3, 0, "in_progress", "next"},
		{3, 3, 1, "in_progress", "ready_for_signoff"},
		{4, 4, 0, "in_progress", "next"},
		{11, 12, 0, "closed", "done"},
	}
	for _, c := range cases {
		got := projectWaveStatus(c.idx, c.done, c.ready, c.status)
		if got != c.want {
			t.Errorf("projectWaveStatus(%d,%d,%d,%q) = %q, want %q", c.idx, c.done, c.ready, c.status, got, c.want)
		}
	}
}

func TestDeriveTradeK8sNextTask(t *testing.T) {
	got := deriveTradeK8sNextTask(3, 0, "in_progress")
	want := "W3 NEXT — Kustomize API component + single image; fix prod config mount alias. (W0–W2 signed)"
	if got != want {
		t.Errorf("next task = %q, want %q", got, want)
	}

	got2 := deriveTradeK8sNextTask(3, 1, "in_progress")
	if got2 == "" || got2[:2] != "W3" {
		t.Errorf("delivered next task should start with W3 DELIVERED, got %q", got2)
	}
}

func TestDeriveFocusHeadlineClosedStream(t *testing.T) {
	ctx := &opscontext.File{
		Tracks: &opscontext.Tracks{
			Migrate: &opscontext.MigrateTrack{
				Streams: []opscontext.MigrateStream{
					{
						ID:              TradeK8sNativeStreamID,
						Total:           12,
						Done:            12,
						ReadyForSignoff: 0,
						Status:          "closed",
					},
				},
			},
		},
	}
	got := deriveFocusHeadline(ctx)
	want := "Trade K8s-native CLOSED — (W0–W11 signed)"
	if got != want {
		t.Errorf("deriveFocusHeadline(closed) = %q, want %q", got, want)
	}
}

func TestDeriveDataLayerNextTask(t *testing.T) {
	got := deriveStreamNextTask(DataLayerK3sStreamID, 6, 0, "in_progress")
	want := "⑦ NEXT — Retire embedded stateful — remove postgres/redis from bifrost-* base; bare .80 PG standby or offline. (①–⑥ complete ✓)"
	if got != want {
		t.Errorf("data-layer next task = %q, want %q", got, want)
	}
}
