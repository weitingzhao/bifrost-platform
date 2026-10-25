package store_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const landingTask = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
const landingAttempt = "a_01ARZ3NDEKTSV4RRFFQ69G5FAV"
const landingRepoStream = "landing:bifrost-platform"

func landingRecord(op, kind string, kv ...any) map[string]any {
	r := map[string]any{"v": 1, "kind": kind, "op": op}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}
func appendLanding(w *world, stream string, r map[string]any) error {
	return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": "Landing protocol test", "landing": r}})
		return err
	})
}
func landingState(t *testing.T, w *world, stream string) landing.State {
	t.Helper()
	var s landing.State
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { var e error; s, e = store.LandingState(ctx, tx, stream); return e }))
	return s
}

// This uses the real migration harness and runtime role. Without PG it skips,
// never substitutes an in-memory database or claims the CHECK was exercised.
// Serial: FreezeEventClock.
func TestLandingAllKindsMigratedDatabase(t *testing.T) {
	w := newWorld(t)
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	clock := at
	t.Cleanup(store.FreezeEventClock(clock))
	seen := map[string]bool{}
	put := func(stream string, r map[string]any) {
		t.Helper()
		clock = clock.Add(time.Minute)
		restore := store.FreezeEventClock(clock)
		defer restore()
		ok(t, appendLanding(w, stream, r))
		seen[r["kind"].(string)] = true
		_ = landingState(t, w, stream)
	}
	put(landingRepoStream, landingRecord("enable", "enabled", "repo", "bifrost-platform", "rule_version", 1))
	prepare := func(op string, dry bool) {
		put(landingRepoStream, landingRecord(op, "claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1))
		put(landingRepoStream, landingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339), "dry_run", dry))
		env := "prod"
		if dry {
			env = "stg"
		}
		ref := "refs/heads/land/" + env + "/" + landingTask + "/1"
		put(landingRepoStream, landingRecord(op, "branch_intent", "ref", ref, "R", commitB))
		put(landingRepoStream, landingRecord(op, "branch_pushed", "ref", ref, "R", commitB))
	}
	prepare("land:1", false)
	put(landingRepoStream, landingRecord("land:1", "parked", "why", "window", "retry_at", clock.Add(time.Hour).Format(time.RFC3339)))
	put(landingRepoStream, landingRecord("land:1", "unparked"))
	put(landingRepoStream, landingRecord("land:1", "ci_kick", "try", 1))
	put(landingRepoStream, landingRecord("land:1", "ci_rerun", "try", 2))
	put(landingRepoStream, landingRecord("land:1", "ci_seen", "run", "ci-1", "status", "Succeeded", "R", commitB, "created_at", clock.Format(time.RFC3339)))
	quota := "landing-quota:dev:" + at.Format("2006-01-02")
	put(quota, landingRecord("land:1", "reserved", "root", landingTask))
	put(landingRepoStream, landingRecord("land:1", "main_intent", "R", commitB, "M0", commitA))
	put(landingRepoStream, landingRecord("land:1", "main_result", "outcome", "ok"))
	put(landingRepoStream, landingRecord("land:1", "landed", "R", commitB))
	put(landingRepoStream, landingRecord("land:1", "reverted", "R", commitB, "by", commitA))
	put(landingRepoStream, landingRecord("land:2", "claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1))
	put(landingRepoStream, landingRecord("land:2", "failed", "why", "declined"))
	for i, outcome := range []string{"ok", "failed"} {
		op := fmt.Sprintf("stg:%d", i)
		if i == 0 {
			put(landingRepoStream, landingRecord(op, "stg_pending", "R", []string{commitB}))
		}
		put(landingRepoStream, landingRecord(op, "stg_snapshot", "P", commitB, "U", commitA, "try", 1, "not_before", clock.Format(time.RFC3339)))
		put(landingRepoStream, landingRecord(op, "stg_started", "run", op, "try", 1))
		put(landingRepoStream, landingRecord(op, "stg_result", "run", op, "outcome", outcome))
		if outcome == "ok" {
			dig := map[string]any{}
			for _, k := range []string{"platform-api", "platform-workers", "platform-console"} {
				dig[k] = "sha256:" + strings.Repeat("a", 64)
			}
			put(landingRepoStream, landingRecord(op, "stg_verified", "P", commitB, "U", commitA, "digests", dig))
		} else {
			put(landingRepoStream, landingRecord(op, "stg_failed", "why", "ci_failed"))
		}
	}
	if len(seen) != 22 {
		t.Fatalf("covered %d kinds, want 22: %v", len(seen), seen)
	}
	s := landingState(t, w, landingRepoStream)
	if !s.Operations["land:1"].EverLanded || !s.Operations["land:1"].Reverted || s.Operations["stg:0"].Phase != "stg_verified" {
		t.Fatal("fold lost state", s)
	}
	if !s.EnabledAt.Equal(at.Add(time.Minute)) {
		t.Fatal("enabled time is not event time")
	}
	for _, stream := range []string{landingRepoStream, quota} {
		_, e := store.VerifyStream(ctx, w.rt, stream)
		ok(t, e)
	}
}
func TestLandingAppendRejectsIllegalTransitions(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	claim := landingRecord("land:1", "claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1)
	bad := []map[string]any{landingRecord("land:1", "landed", "R", commitB), landingRecord("land:1", "unparked"), landingRecord("land:1", "main_result", "outcome", "ok"), landingRecord("land:1", "failed", "why", "declined")}
	for _, r := range bad {
		if appendLanding(w, landingRepoStream, r) == nil {
			t.Fatal("illegal append", r)
		}
	}
	ok(t, appendLanding(w, landingRepoStream, claim))
	if appendLanding(w, landingRepoStream, claim) == nil {
		t.Fatal("claim repeated")
	}
	before := landingState(t, w, landingRepoStream)
	for _, r := range bad[:3] {
		if appendLanding(w, landingRepoStream, r) == nil {
			t.Fatal("illegal append after claim", r)
		}
	}
	if after := landingState(t, w, landingRepoStream); after.Operations["land:1"].Phase != before.Operations["land:1"].Phase {
		t.Fatal("refusal changed state")
	}
	if appendLanding(w, store.MissionStream("M-1"), landingRecord("land:2", "claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1)) == nil {
		t.Fatal("mission mirror accepted protocol")
	}
}
func TestLandingDryRunCannotReserveOrLand(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	op := "dry:1"
	ref := "refs/heads/land/stg/" + landingTask + "/1"
	for _, r := range []map[string]any{landingRecord(op, "claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1), landingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", time.Now().Format(time.RFC3339), "dry_run", true), landingRecord(op, "branch_intent", "ref", ref, "R", commitB), landingRecord(op, "branch_pushed", "ref", ref, "R", commitB)} {
		ok(t, appendLanding(w, landingRepoStream, r))
	}
	quota := "landing-quota:dev:" + time.Now().UTC().Format("2006-01-02")
	if appendLanding(w, quota, landingRecord(op, "reserved", "root", landingTask)) == nil {
		t.Fatal("dry run counted")
	}
	if appendLanding(w, landingRepoStream, landingRecord(op, "landed", "R", commitB)) == nil {
		t.Fatal("dry run landed")
	}
	if len(landingState(t, w, quota).Reservations) != 0 {
		t.Fatal("refused reservation persisted")
	}
}
