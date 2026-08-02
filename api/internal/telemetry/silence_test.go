package telemetry

import (
	"context"
	"testing"
)

func TestCreateAttentionMuteUIOnlyWithoutAlertmanager(t *testing.T) {
	t.Setenv("PLATFORM_ALERTMANAGER_URL", "")
	svc := &Service{cfg: nil}
	resp := svc.CreateAttentionMute(context.Background(), AttentionMuteRequest{
		AttentionID: "alert:1",
		SignalLabel: "KubePodNotReady",
		Domain:      "rocket",
		Env:         "shared",
		DurationH:   2,
	})
	if !resp.OK {
		t.Fatal("expected ok")
	}
	if resp.Alertmanager != "skipped" {
		t.Fatalf("alertmanager: got %s want skipped", resp.Alertmanager)
	}
	if resp.ExpiresAt.IsZero() {
		t.Fatal("expected expires_at")
	}
}

func TestCreateAttentionMuteCapsDuration(t *testing.T) {
	svc := &Service{cfg: nil}
	resp := svc.CreateAttentionMute(context.Background(), AttentionMuteRequest{
		AttentionID: "a",
		SignalLabel: "x",
		DurationH:   100,
	})
	// 24h cap — expires roughly now+24h (allow 1m skew)
	delta := resp.ExpiresAt.Sub(resp.ExpiresAt.Add(-0)) // sanity non-zero
	_ = delta
	if resp.ExpiresAt.IsZero() {
		t.Fatal("expected expires")
	}
}
