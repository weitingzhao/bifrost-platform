package threadtitles

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func writeTranscript(t *testing.T, dir, project, id string, lines ...string) string {
	t.Helper()
	d := filepath.Join(dir, project)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, id+".jsonl")
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanPicksLastCustomTitleElseAITitle(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, dir, "proj", "aaa",
		`{"type":"ai-title","aiTitle":"generated","sessionId":"aaa"}`,
		`{"type":"user","message":{"content":"has \"type\":\"custom-title\" inside text"}}`,
		`{"type":"custom-title","customTitle":"First name","sessionId":"aaa"}`,
		`{"type":"custom-title","customTitle":"Renamed","sessionId":"aaa"}`,
	)
	writeTranscript(t, dir, "proj", "bbb", `{"type":"ai-title","aiTitle":"Only generated","sessionId":"bbb"}`)
	writeTranscript(t, dir, "proj", "ccc", `{"type":"user"}`)
	sc := NewScanner(dir, 24*time.Hour)
	got, err := sc.Scan(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got["aaa"].Title != "Renamed" || got["bbb"].Title != "Only generated" || len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	// unchanged file: served from the cache; old file: skipped
	old := filepath.Join(dir, "proj", "bbb.jsonl")
	past := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(old, past, past)
	got, _ = sc.Scan(time.Now())
	if _, ok := got["bbb"]; ok || got["aaa"].Title != "Renamed" {
		t.Fatalf("after aging = %+v", got)
	}
}

func TestStoreManualAndSynced(t *testing.T) {
	core := k8sfake.NewSimpleClientset()
	s := NewStore("cicd", func() (kubernetes.Interface, error) { return core, nil })
	ctx := context.Background()
	if tt, err := s.Load(ctx); err != nil || len(tt.Manual)+len(tt.Transcripts) != 0 {
		t.Fatalf("empty load = %+v %v", tt, err)
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if n, err := s.MergeTranscripts(ctx, map[string]Entry{"t1": {Title: "A", At: now}}); err != nil || n != 1 {
		t.Fatalf("merge = %d %v", n, err)
	}
	if n, _ := s.MergeTranscripts(ctx, map[string]Entry{"t1": {Title: "A", At: now.Add(time.Hour)}}); n != 0 {
		t.Fatalf("unchanged title rewrote: %d", n)
	}
	if err := s.SetManual(ctx, "local_x", "  My thread  ", now); err != nil {
		t.Fatal(err)
	}
	tt, _ := s.Load(ctx)
	if tt.Manual["local_x"].Title != "My thread" || tt.Transcripts["t1"].Title != "A" {
		t.Fatalf("load = %+v", tt)
	}
	if err := s.SetManual(ctx, "local_x", "", now); err != nil {
		t.Fatal(err)
	}
	if tt, _ := s.Load(ctx); len(tt.Manual) != 0 || tt.Transcripts["t1"].Title != "A" {
		t.Fatalf("after clear = %+v", tt)
	}
	if err := s.SetManual(ctx, "", "x", now); err == nil {
		t.Fatal("empty session accepted")
	}
}

func TestSyncWanted(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		flag, k8s, dir string
		want           bool
	}{
		{"", "", dir, true},
		{"", "", filepath.Join(dir, "missing"), false},
		{"", "10.43.0.1", dir, false},
		{"on", "10.43.0.1", dir, true},
		{"off", "", dir, false},
	} {
		t.Setenv("PLATFORM_THREAD_TITLE_SYNC", c.flag)
		t.Setenv("KUBERNETES_SERVICE_HOST", c.k8s)
		if got := SyncWanted(c.dir); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}
