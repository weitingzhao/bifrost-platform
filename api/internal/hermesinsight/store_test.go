package hermesinsight

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestStoreEmptyList(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	items, total := s.List(50)
	if total != 0 {
		t.Fatalf("total=%d", total)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("items=%v", items)
	}
}

func TestStoreAppendNewestFirstAndRing(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxInsights+5; i++ {
		if err := s.Append(HermesInsight{
			ID:      "id-" + strconv.Itoa(i),
			Time:    time.Unix(int64(i), 0).UTC().Format(time.RFC3339),
			Type:    TypeFirstTask,
			Verdict: VerdictOK,
			Source:  SourceFirstTask,
		}); err != nil {
			t.Fatal(err)
		}
	}
	items, total := s.List(10)
	if total != MaxInsights {
		t.Fatalf("total=%d want %d", total, MaxInsights)
	}
	if len(items) != 10 {
		t.Fatalf("len=%d", len(items))
	}
	if items[0].ID != "id-"+strconv.Itoa(MaxInsights+4) {
		t.Fatalf("newest id=%s", items[0].ID)
	}
}

func TestStoreReload(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(HermesInsight{ID: "keep", Type: TypeFirstTask, Verdict: VerdictOK, Source: SourceFirstTask}); err != nil {
		t.Fatal(err)
	}
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	items, total := s2.List(10)
	if total != 1 || items[0].ID != "keep" {
		t.Fatalf("reload items=%v total=%d", items, total)
	}
}

func TestDefaultStateDirEnv(t *testing.T) {
	t.Setenv("HERMES_INSIGHT_STATE_DIR", "/tmp/hermes-insight-override")
	t.Setenv("PLATFORM_DATA_DIR", "/tmp/platform-data-should-not-win")
	if got := DefaultStateDir(); got != "/tmp/hermes-insight-override" {
		t.Fatalf("dir=%s", got)
	}
	t.Setenv("HERMES_INSIGHT_STATE_DIR", "")
	t.Setenv("PLATFORM_DATA_DIR", "/tmp/platform-data")
	if got := DefaultStateDir(); got != filepath.Join("/tmp/platform-data", "hermes-insights") {
		t.Fatalf("dir=%s", got)
	}
}
