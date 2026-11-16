package cirunview

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
)

func TestFullCINotePresentationPreservesWireShape(t *testing.T) {
	t.Parallel()
	empty, err := json.Marshal(FullCIAnswer(nil))
	if err != nil || string(empty) != `{"runs":[]}` {
		t.Fatalf("empty: %s %v", empty, err)
	}
	notes := []evidence.FullCINote{
		{At: time.Date(2026, 11, 15, 18, 0, 0, 900, time.FixedZone("local", -6*60*60)), Text: "latest", Body: map[string]any{"run": "ci-one", "failed_tasks": []string{"api"}}},
		{At: time.Date(2026, 11, 15, 23, 0, 0, 0, time.UTC), Text: "earlier", Body: nil},
	}
	raw, err := json.Marshal(FullCIAnswer(notes))
	want := `{"runs":[{"at":"2026-11-16T00:00:00Z","full_ci":{"failed_tasks":["api"],"run":"ci-one"},"text":"latest"},{"at":"2026-11-15T23:00:00Z","full_ci":null,"text":"earlier"}]}`
	if err != nil || string(raw) != want {
		t.Fatalf("changed order/time/body: %s %v", raw, err)
	}
}
