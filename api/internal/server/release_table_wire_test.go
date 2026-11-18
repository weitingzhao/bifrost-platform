//bifrost:owner delivery

package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	busreleases "github.com/weitingzhao/bifrost-platform/api/internal/bus/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// 2026-11-17, step 1 of moving the release records into the database
// (agentrt 0027, on PROD by #589/#591): a record is a row and an
// announcement written in one transaction, the row naming its event. A
// record whose revision no single repository built is a row without one
// (event_id NULL). Positive control: with the Append call removed from
// releaseTable the first row's event_id is NULL and the test fails.
func TestReleaseTableWritesTheRowWithItsAnnouncement(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	write := releaseTable(pool)
	sha := strings.Repeat("a", 40)
	at := time.Date(2026, 11, 17, 17, 20, 0, 0, time.UTC)
	rec := releases.Record{
		Run: "bifrost-deliver-platform-prod-f1bc55557754b310-1", Pipeline: "bifrost-deliver-platform-prod",
		Lane: "platform", Env: "prod", Deploys: true, Revision: sha,
		StartedAt: at, CompletedAt: at.Add(88 * time.Second),
		Repos:  map[string]releases.RepoBuild{"bifrost-platform": {SHA: sha, Source: "param"}, "bifrost-ui": {SHA: strings.Repeat("c", 40), Source: "param"}},
		Images: map[string]string{"platform-api": "sha256:56f9e9ad"},
	}
	if err := write(ctx, rec, true); err != nil {
		t.Fatal(err)
	}
	unannounced := rec
	unannounced.Run = "bifrost-build-apis-dev-x1"
	unannounced.Revision = ""
	if err := write(ctx, unannounced, true); err != nil {
		t.Fatal(err)
	}

	var eventID *int64
	var repo, subject string
	if err := pool.QueryRow(ctx, `SELECT r.event_id, r.repos->'bifrost-platform'->>'sha', e.body_json->>'subject'
		FROM agentrt.release_record r JOIN agentrt.event e ON e.event_id = r.event_id
		WHERE r.run = $1`, rec.Run).Scan(&eventID, &repo, &subject); err != nil {
		t.Fatal("the row and its announcement:", err)
	}
	if eventID == nil || repo != sha || subject != "prod/platform@"+sha {
		t.Fatalf("event_id=%v repo sha=%q subject=%q", eventID, repo, subject)
	}
	var none *int64
	if err := pool.QueryRow(ctx, `SELECT event_id FROM agentrt.release_record WHERE run = $1`, unannounced.Run).Scan(&none); err != nil || none != nil {
		t.Fatalf("a record naming no repository was announced or not written: %v %v", none, err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1`, busreleases.Type).Scan(&n); err != nil || n != 1 {
		t.Fatalf("announcements=%d err=%v", n, err)
	}

	// The communication line, 2026-11-17: a record built again (a commit
	// readable now, or a newer record version) is a new row of the same
	// release; announcing it again would make one release two. Positive
	// control: without the PriorEvent check the count below is 2.
	rebuilt := rec
	rebuilt.Missing = nil
	rebuilt.Images = map[string]string{"platform-api": "sha256:56f9e9ad", "platform-console": "sha256:a713fb36"}
	if err := write(ctx, rebuilt, true); err != nil {
		t.Fatal(err)
	}
	var rows int
	var named []int64
	if err := pool.QueryRow(ctx, `SELECT count(*), array_agg(event_id) FROM agentrt.release_record WHERE run = $1`, rec.Run).Scan(&rows, &named); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1`, busreleases.Type).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || n != 1 || len(named) != 2 || named[0] != *eventID || named[1] != *eventID {
		t.Fatalf("rows=%d announcements=%d events named=%v first=%d", rows, n, named, *eventID)
	}
}

// 2026-11-17, step 2 (the side-by-side reading before Current reads the
// table): a ConfigMap record from before the rows is copied in without an
// announcement, once; the rows read back as the records they were written
// from, newest row of each run. Positive control: BackfillRecord without its
// PriorEvent check writes the second copy and the count below is 2.
func TestReleaseRowsBackfillWithoutAnnouncingAndReadBack(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	rows := releaseRows{pool: pool}
	sha := strings.Repeat("b", 40)
	at := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	old := releases.Record{
		Run: "bifrost-deliver-platform-prod-1794800000", Pipeline: "bifrost-deliver-platform-prod",
		Lane: "platform", Env: "prod", Deploys: true, Revision: sha,
		StartedAt: at, CompletedAt: at.Add(80 * time.Second),
		Repos:   map[string]releases.RepoBuild{"bifrost-platform": {SHA: sha, Source: "param"}},
		Missing: []string{"build-x (digest)"},
	}
	for i, want := range []bool{true, false} {
		wrote, err := rows.Backfill(ctx, old)
		if err != nil || wrote != want {
			t.Fatalf("backfill %d: wrote=%v err=%v", i, wrote, err)
		}
	}
	var n, announced int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(event_id) FROM agentrt.release_record WHERE run = $1`, old.Run).Scan(&n, &announced); err != nil || n != 1 || announced != 0 {
		t.Fatalf("rows=%d announced=%d err=%v", n, announced, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1`, busreleases.Type).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a backfilled record was announced: %d %v", n, err)
	}
	got, err := rows.Records(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("records=%+v err=%v", got, err)
	}
	r := got[0]
	if r.Run != old.Run || r.Lane != "platform" || r.Env != "prod" || !r.Deploys || r.Repos["bifrost-platform"].SHA != sha ||
		!r.CompletedAt.Equal(old.CompletedAt) || len(r.Missing) != 1 {
		t.Fatalf("read back %+v", r)
	}
}

// 2026-11-17, with the communication layer: release.deploy.finished names, as
// its cause, the ledger event the run's cause label names (the
// git.main.advanced that woke head-release), so the chain can be followed on
// the bus. A label naming an event this ledger does not have is left out,
// and the row is still written. Positive control: with Cause dropped from
// busreleases.Append the first announcement's cause_event_id is NULL.
func TestReleaseTableNamesTheCauseOfTheRun(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	write := releaseTable(pool)
	sha := strings.Repeat("d", 40)
	at := time.Date(2026, 11, 17, 22, 0, 0, 0, time.UTC)
	base := releases.Record{
		Run: "bifrost-deliver-platform-1794952405", Pipeline: "bifrost-deliver-platform",
		Lane: "platform", Env: "stg", Deploys: true, Revision: sha,
		StartedAt: at, CompletedAt: at.Add(3 * time.Minute),
		Repos: map[string]releases.RepoBuild{"bifrost-platform": {SHA: sha, Source: "param"}},
	}
	// an earlier announcement stands in for the git.main.advanced that woke the start
	if err := write(ctx, base, true); err != nil {
		t.Fatal(err)
	}
	var woke int64
	if err := pool.QueryRow(ctx, `SELECT event_id FROM agentrt.release_record WHERE run = $1`, base.Run).Scan(&woke); err != nil {
		t.Fatal(err)
	}
	caused := base
	caused.Run, caused.Cause = "bifrost-deliver-platform-1794952406", fmt.Sprintf("ev-%d", woke)
	unknown := base
	unknown.Run, unknown.Cause = "bifrost-deliver-platform-1794952407", "ev-999999999"
	for _, rec := range []releases.Record{caused, unknown} {
		if err := write(ctx, rec, true); err != nil {
			t.Fatalf("%s: %v", rec.Run, err)
		}
	}
	for run, want := range map[string]*int64{caused.Run: &woke, unknown.Run: nil} {
		var cause *int64
		if err := pool.QueryRow(ctx, `SELECT e.cause_event_id FROM agentrt.release_record r JOIN agentrt.event e ON e.event_id = r.event_id
			WHERE r.run = $1`, run).Scan(&cause); err != nil {
			t.Fatalf("%s: %v", run, err)
		}
		if (want == nil) != (cause == nil) || (want != nil && *cause != *want) {
			t.Fatalf("%s: cause %v, want %v", run, cause, want)
		}
	}
}
