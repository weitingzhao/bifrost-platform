package opscontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchMigrateStream(t *testing.T) {
	src := filepath.Join("..", "..", "..", "config", "ops-context.yaml")
	if _, err := os.Stat(src); os.IsNotExist(err) {
		t.Skip("config/ops-context.yaml not found")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "ops-context.yaml")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatal(err)
	}

	headline := "Trade K8s-native W3 NEXT — test headline"
	nextTask := "W3 NEXT — test task. (W0–W2 signed)"
	if err := PatchMigrateStream(tmp, MigrateStreamPatch{
		StreamID:        "trade-k8s-native",
		Done:            3,
		ReadyForSignoff: 1,
		Status:          "in_progress",
		NextTask:        nextTask,
		Note:            "test note",
		Headline:        headline,
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	var stream *MigrateStream
	for i := range loaded.Tracks.Migrate.Streams {
		if loaded.Tracks.Migrate.Streams[i].ID == "trade-k8s-native" {
			stream = &loaded.Tracks.Migrate.Streams[i]
			break
		}
	}
	if stream == nil {
		t.Fatal("trade-k8s-native stream not found after patch")
	}
	if stream.Done != 3 || stream.ReadyForSignoff != 1 {
		t.Fatalf("done/ready = %d/%d", stream.Done, stream.ReadyForSignoff)
	}
	if stream.NextTask == nil || *stream.NextTask != nextTask {
		t.Fatalf("next_task = %v", stream.NextTask)
	}
	if loaded.Focus.Headline != headline {
		t.Fatalf("headline = %q", loaded.Focus.Headline)
	}
	if !strings.Contains(stream.Note, "test note") {
		t.Fatalf("note = %q", stream.Note)
	}
}
