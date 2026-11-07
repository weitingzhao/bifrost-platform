package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The agentd build a host's claims report is recorded when it changes, and
// the hosts document the Console reads says the last one and since when; a
// build that is not a commit and a sha256 is refused.
func TestAHostsBuildIsRecordedAndReadBack(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	w.poll(t, "h1", claude)
	if doc := hostBuilds(t, w); doc["h1"].Build != nil || doc["h1"].BuildAt != nil {
		t.Fatalf("a build before any is reported: %+v", doc["h1"])
	}
	old := store.AgentdBuild{Commit: strings.Repeat("0a", 20), SHA256: strings.Repeat("b1", 32)}
	for i, want := range []bool{true, false} {
		recorded, err := store.RecordHostBuild(ctx, w.rt, "h1", old)
		if ok(t, err); recorded != want {
			t.Fatalf("report %d of one build recorded %v", i, recorded)
		}
	}
	first := hostBuilds(t, w)["h1"]
	if first.Build == nil || *first.Build != old || first.BuildAt == nil {
		t.Fatalf("h1 after its first build: %+v", first)
	}
	deployed := store.AgentdBuild{Commit: strings.Repeat("1c", 20), SHA256: strings.Repeat("d2", 32), Modified: true}
	recorded, err := store.RecordHostBuild(ctx, w.rt, "h1", deployed)
	if ok(t, err); !recorded {
		t.Fatal("a new build was not recorded")
	}
	now := hostBuilds(t, w)["h1"]
	if now.Build == nil || *now.Build != deployed || now.BuildAt == nil || now.BuildAt.Before(*first.BuildAt) {
		t.Fatalf("h1 after a deploy: %+v (first %+v)", now, first)
	}
	for _, bad := range []store.AgentdBuild{{Commit: "main", SHA256: deployed.SHA256}, {Commit: deployed.Commit, SHA256: "D2"}} {
		if _, err := store.RecordHostBuild(ctx, w.rt, "h1", bad); err == nil {
			t.Fatalf("a bad build recorded: %+v", bad)
		}
	}
	if err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.HostBuildStream("h1"), Type: "note", Actor: "controller",
			Body: map[string]any{"text": "x", "agentd_build": map[string]any{"commit": deployed.Commit}}})
		return err
	}); err == nil {
		t.Fatal("a note with half a build was recorded")
	}
}

// hostBuilds reads the hosts as Progress (and so GET /hosts) has them.
func hostBuilds(t *testing.T, w *world) map[string]store.HostPresence {
	t.Helper()
	_, raw := w.presence(t)
	var doc struct {
		Hosts []store.HostPresence `json:"hosts"`
	}
	ok(t, json.Unmarshal(raw, &doc))
	out := map[string]store.HostPresence{}
	for _, h := range doc.Hosts {
		out[h.HostID] = h
	}
	return out
}
