package threadtitles

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestStoreManualAndReported(t *testing.T) {
	core := k8sfake.NewSimpleClientset()
	s := NewStore("cicd", func() (kubernetes.Interface, error) { return core, nil })
	ctx := context.Background()
	if tt, err := s.Load(ctx); err != nil || len(tt.Manual)+len(tt.Transcripts) != 0 {
		t.Fatalf("empty load = %+v %v", tt, err)
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if err := s.ReportTranscript(ctx, "11111111-2222-3333-4444-555555555555", "A", now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetManual(ctx, "local_x", "  My thread  ", now); err != nil {
		t.Fatal(err)
	}
	tt, _ := s.Load(ctx)
	if tt.Manual["local_x"].Title != "My thread" || tt.Transcripts["11111111-2222-3333-4444-555555555555"].Title != "A" {
		t.Fatalf("load = %+v", tt)
	}
	if err := s.SetManual(ctx, "local_x", "", now); err != nil {
		t.Fatal(err)
	}
	if tt, _ := s.Load(ctx); len(tt.Manual) != 0 || tt.Transcripts["11111111-2222-3333-4444-555555555555"].Title != "A" {
		t.Fatalf("after clear = %+v", tt)
	}
	if err := s.SetManual(ctx, "", "x", now); err == nil {
		t.Fatal("empty session accepted")
	}
}

func TestReportTranscript(t *testing.T) {
	core := k8sfake.NewSimpleClientset()
	s := NewStore("cicd", func() (kubernetes.Interface, error) { return core, nil })
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	const id = "d09a336c-ef6d-4b7c-abc6-d7e907e428f4"
	if err := s.ReportTranscript(ctx, id, "  Renamed in Desktop ", now); err != nil {
		t.Fatal(err)
	}
	if tt, _ := s.Load(ctx); tt.Transcripts[id].Title != "Renamed in Desktop" || !tt.Transcripts[id].At.Equal(now) {
		t.Fatalf("load = %+v", tt.Transcripts)
	}
	for _, bad := range []struct{ id, title string }{{"not-a-uuid", "x"}, {id, ""}, {id, strings.Repeat("x", 121)}} {
		if err := s.ReportTranscript(ctx, bad.id, bad.title, now); err == nil {
			t.Errorf("accepted %q / %d chars", bad.id, len(bad.title))
		}
	}
}
