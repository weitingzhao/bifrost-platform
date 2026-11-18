package drift_test

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor" // the seed (the controller actor), in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/drift"
)

var ctx = context.Background()

// Origin: 2026-11-16, the incremental landing reopened. A full run of the
// push to main that fails where the landing's incremental run passed has to
// reach the line that pushed the commit; the main coordinator ruled it goes on
// the bus's own topic for a reconciliation that found a difference. One drift
// is one event whose subject names the repository and the commit.
func TestAnnounceWritesTheDriftOnce(t *testing.T) {
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	f := drift.Found{Repo: "bifrost-platform", Commit: strings.Repeat("a", 40), Kind: drift.FullVsIncremental,
		Passed: "ci-platform-ready1", Contradicts: "ci-platform-main1"}
	if ok, err := drift.Announce(ctx, pool, f); err != nil || !ok {
		t.Fatalf("announce: %v %v", ok, err)
	}
	var subject, ref, key, change string
	if err := pool.QueryRow(ctx, `SELECT body_json->>'subject', body_json->>'ref', body_json->>'key', body_json->>'change' FROM agentrt.event WHERE event_type = $1`, drift.Type).
		Scan(&subject, &ref, &key, &change); err != nil {
		t.Fatal(err)
	}
	if subject != "bifrost-platform@"+f.Commit+":full-vs-incremental" || key != "bifrost-platform" || change != "found" ||
		ref != "/api/v1/evidence/ci?repo=bifrost-platform&commit="+f.Commit {
		t.Fatalf("event: %q %q %q %q", subject, ref, key, change)
	}
	// Controls: what the bus cannot name is not written, and says so.
	for name, bad := range map[string]drift.Found{
		"the same run twice": {Repo: f.Repo, Commit: f.Commit, Kind: f.Kind, Passed: "ci-1", Contradicts: "ci-1"},
		"not a commit":       {Repo: f.Repo, Commit: "main", Kind: f.Kind, Passed: "ci-1", Contradicts: "ci-2"},
		"another kind":       {Repo: f.Repo, Commit: f.Commit, Kind: "anything", Passed: "ci-1", Contradicts: "ci-2"},
	} {
		if ok, err := drift.Announce(ctx, pool, bad); err != nil || ok {
			t.Errorf("%s: announced %v, %v", name, ok, err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1`, drift.Type).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d events, %v", n, err)
	}
}

// A ledger the platform has not seeded announces nothing and fails nothing.
func TestAnnounceWithoutTheControllerIsSilent(t *testing.T) {
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	f := drift.Found{Repo: "bifrost-platform", Commit: strings.Repeat("b", 40), Kind: drift.FullVsIncremental, Passed: "ci-1", Contradicts: "ci-2"}
	if ok, err := drift.Announce(ctx, pool, f); err != nil || ok {
		t.Fatalf("announced %v, %v", ok, err)
	}
}
