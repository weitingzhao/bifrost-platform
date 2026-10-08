package maintainer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEveryWorkerLoopRecordsTheMetric(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(Loops()) == 0 {
		t.Fatal("startup inventory is empty")
	}
	seen := map[string]bool{}
	for _, loop := range Loops() {
		if loop.Name == "" || loop.Symbol == "" || loop.File == "" {
			t.Fatalf("incomplete loop entry: %+v", loop)
		}
		if seen[loop.Name] {
			t.Fatalf("duplicate loop %s", loop.Name)
		}
		seen[loop.Name] = true
		path := filepath.Join(root, loop.File)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", loop.File, err)
		}
		text := string(data)
		if !strings.Contains(text, loop.Symbol) {
			t.Errorf("%s does not reference maintainer.%s", loop.File, loop.Symbol)
		}
		if !strings.Contains(text, "maintainer.Success") {
			t.Errorf("%s does not call maintainer.Success", loop.File)
		}
	}
}

func TestSuccessAndFailureExposition(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	prev := now
	now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	t.Cleanup(func() { now = prev })

	t.Setenv("OPS_VIEWER_ENV", "prod")
	id := PlatformID(LoopIBAutoRepair)
	if id != "platform/prod/ib-autorepair" {
		t.Fatalf("id = %s", id)
	}
	Failure(id)
	var b strings.Builder
	Write(&b)
	if strings.Contains(b.String(), "bifrost_maintainer_last_success_timestamp_seconds{") {
		t.Fatalf("failure must not emit the gauge, got:\n%s", b.String())
	}
	if !strings.Contains(b.String(), `bifrost_maintainer_runs_total{maintainer="platform/prod/ib-autorepair",result="failure"} 1`) {
		t.Fatalf("counter missing:\n%s", b.String())
	}
	Success(id)
	b.Reset()
	Write(&b)
	if !strings.Contains(b.String(), `bifrost_maintainer_last_success_timestamp_seconds{maintainer="platform/prod/ib-autorepair"} 1700000000`) {
		t.Fatalf("gauge missing:\n%s", b.String())
	}
	if !strings.Contains(b.String(), `result="success"} 1`) {
		t.Fatalf("success counter missing:\n%s", b.String())
	}
}

func TestPlatformIDLocalWhenUnset(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "")
	if got := PlatformID(LoopDataClone); got != "platform/local/data-clone-scheduler" {
		t.Fatalf("got %s", got)
	}
}
