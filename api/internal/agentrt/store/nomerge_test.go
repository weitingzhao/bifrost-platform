package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const noMergeApproval = "appr_0123456789abcdef"

func noMergeRequest(missions ...store.NoMergeMission) store.NoMergeRequest {
	return store.NoMergeRequest{Env: "stg", Missions: missions, Reason: "archived drill; disposed, not delivered", ArchiveManifestSHA256: strings.Repeat("ab", 32)}
}

func TestNoMergeRequestBounds(t *testing.T) {
	t.Parallel()
	valid := func() store.NoMergeRequest {
		return noMergeRequest(store.NoMergeMission{MissionID: "M-1", ExpectedVersion: 1})
	}
	for name, change := range map[string]func(*store.NoMergeRequest){
		"env":       func(r *store.NoMergeRequest) { r.Env = "dev" },
		"empty":     func(r *store.NoMergeRequest) { r.Missions = nil },
		"duplicate": func(r *store.NoMergeRequest) { r.Missions = append(r.Missions, r.Missions[0]) },
		"version":   func(r *store.NoMergeRequest) { r.Missions[0].ExpectedVersion = 0 },
		"id":        func(r *store.NoMergeRequest) { r.Missions[0].MissionID = " " },
		"reason":    func(r *store.NoMergeRequest) { r.Reason = " \n" },
		"bytes":     func(r *store.NoMergeRequest) { r.Reason = strings.Repeat("目", store.MaxNoMergeBytes/3+1) },
		"digest":    func(r *store.NoMergeRequest) { r.ArchiveManifestSHA256 = strings.Repeat("g", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			r := valid()
			change(&r)
			if !errors.Is(r.Validate(), store.ErrInvalid) {
				t.Fatal("accepted invalid request", r)
			}
		})
	}
	r := valid()
	r.Reason = strings.Repeat("x", store.MaxNoMergeBytes)
	ok(t, r.Validate())
}

func (w *world) noMergeVersion(t *testing.T, id string) int64 {
	t.Helper()
	var v int64
	ok(t, w.rt.QueryRow(ctx, `SELECT version FROM agentrt.mission WHERE mission_id=$1`, id).Scan(&v))
	return v
}

func (w *world) finishNoMergeMission(t *testing.T, id string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.MoveMission(ctx, tx, "controller", id, "done", "test completed")
	}))
}

func TestNoMergeEventVersionRetryAndReaders(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, impl, "h1", "h2", "h3")
	w.finishNoMergeMission(t, "M-1")
	v := w.noMergeVersion(t, "M-1")
	r := noMergeRequest(store.NoMergeMission{MissionID: "M-1", ExpectedVersion: v})
	stream := store.MissionStream("M-1")
	seq := w.lastSeq(t, stream)
	later := time.Now().Add(store.PruneAfter + time.Hour)
	heads := []store.AgentHead{head(impl, 1, commitA)}
	before, err := store.Prunable(ctx, w.rt, heads, nil, later)
	ok(t, err)
	if len(before) != 0 {
		t.Fatal("unlanded result pruned before no_merge", before)
	}
	out, err := store.MarkMissionsNoMerge(ctx, w.rt, r, noMergeApproval)
	ok(t, err)
	if len(out) != 1 || out[0].Version != v+1 || out[0].EventID == 0 || out[0].Duplicate || w.lastSeq(t, stream) != seq+1 {
		t.Fatal("wrong write", out)
	}
	var body map[string]any
	var actor, state string
	var sameScope bool
	ok(t, w.rt.QueryRow(ctx, `SELECT actor_id, body_json FROM agentrt.event WHERE event_id=$1`, out[0].EventID).Scan(&actor, &body))
	if actor != "controller" || body["approval_id"] != noMergeApproval || body["archive_manifest_sha256"] != r.ArchiveManifestSHA256 || body["no_merge"] != r.Reason || body["reason"] != r.Reason || body["env"] != r.Env {
		t.Fatal("missing event evidence", actor, body)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT state, scope_json-'no_merge' = '{"max_action":"branch"}'::jsonb FROM agentrt.mission WHERE mission_id='M-1'`).Scan(&state, &sameScope))
	if state != "done" || !sameScope {
		t.Fatal("changed unrelated mission fields")
	}
	// Retry with the original expected version is allowed only for this exact write.
	again, err := store.MarkMissionsNoMerge(ctx, w.rt, r, noMergeApproval)
	ok(t, err)
	if !again[0].Duplicate || again[0].EventID != out[0].EventID || w.noMergeVersion(t, "M-1") != v+1 || w.lastSeq(t, stream) != seq+1 {
		t.Fatal("retry wrote again", again)
	}
	if _, err = store.MarkMissionsNoMerge(ctx, w.rt, r, "appr_1111111111111111"); !errors.Is(err, store.ErrState) {
		t.Fatal("stale version accepted", err)
	}
	changed := r
	changed.ArchiveManifestSHA256 = strings.Repeat("cd", 32)
	if _, err = store.MarkMissionsNoMerge(ctx, w.rt, changed, noMergeApproval); !errors.Is(err, store.ErrState) {
		t.Fatal("changed evidence accepted as retry", err)
	}
	r.Missions[0].ExpectedVersion++
	again, err = store.MarkMissionsNoMerge(ctx, w.rt, r, "appr_1111111111111111")
	ok(t, err)
	if !again[0].Duplicate || w.lastSeq(t, stream) != seq+1 {
		t.Fatal("equal value was not idempotent")
	}
	r.Reason = "a different disposition"
	if _, err = store.MarkMissionsNoMerge(ctx, w.rt, r, noMergeApproval); !errors.Is(err, store.ErrState) {
		t.Fatal("overwrote no_merge", err)
	}
	_, err = store.VerifyStream(ctx, w.rt, stream)
	ok(t, err)
	src := store.LandingSource{
		Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
			return true, nil, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			return landing.MainHistory{State: landing.Never}, nil
		},
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		f, e := store.LandingFacts(ctx, tx, src, "stg", "drill", impl, "", "", time.Now())
		if e == nil && (!f.NoMerge || int64(f.MissionVersion) != v+1 || f.TaskState != "accepted") {
			t.Fatal("landing missed no_merge or task changed", f)
		}
		return e
	}))
	prunes, err := store.Prunable(ctx, w.rt, heads, nil, later)
	ok(t, err)
	if len(prunes) != 1 || !strings.Contains(prunes[0].Why, "its mission is not merged: archived drill") {
		t.Fatal("prune missed no_merge", prunes)
	}
}

func TestNoMergeBatchRollsBackAllChanges(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.finishNoMergeMission(t, "M-1")
	v := w.noMergeVersion(t, "M-1")
	seq := w.lastSeq(t, store.MissionStream("M-1"))
	// M-1 is processed first. M-2 is still proposed: its refusal must roll
	// back M-1's scope, version AND event stream head.
	r := noMergeRequest(store.NoMergeMission{MissionID: "M-2", ExpectedVersion: 1}, store.NoMergeMission{MissionID: "M-1", ExpectedVersion: v})
	assertRollback := func(req store.NoMergeRequest) {
		t.Helper()
		if out, err := store.MarkMissionsNoMerge(ctx, w.rt, req, noMergeApproval); err == nil || out != nil {
			t.Fatal("expected refusal with no partial result", out, err)
		}
		var has bool
		ok(t, w.rt.QueryRow(ctx, `SELECT scope_json ? 'no_merge' FROM agentrt.mission WHERE mission_id='M-1'`).Scan(&has))
		if has || w.noMergeVersion(t, "M-1") != v || w.lastSeq(t, store.MissionStream("M-1")) != seq {
			t.Fatal("partial batch committed")
		}
	}
	assertRollback(r)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	w.finishNoMergeMission(t, "M-2")
	assertRollback(r) // now done, but M-2's expected version is stale
	r.Missions[0].ExpectedVersion = w.noMergeVersion(t, "M-2")
	out, err := store.MarkMissionsNoMerge(ctx, w.rt, r, noMergeApproval)
	ok(t, err)
	if len(out) != 2 {
		t.Fatal(out)
	}
	for _, m := range out {
		_, err := store.VerifyStream(ctx, w.rt, store.MissionStream(m.MissionID))
		ok(t, err)
	}
}

func TestNoMergeRefusesAnyPresentUnequalJSON(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`null`, `""`, `false`, `123`, `{"reason":"old"}`} {
		t.Run(value, func(t *testing.T) {
			w := newKernelWorld(t)
			w.finishNoMergeMission(t, "M-1")
			w.legacy(t, `UPDATE agentrt.mission SET scope_json=scope_json || '{"no_merge":`+value+`}'::jsonb WHERE mission_id='M-1'`)
			r := noMergeRequest(store.NoMergeMission{MissionID: "M-1", ExpectedVersion: w.noMergeVersion(t, "M-1")})
			if _, err := store.MarkMissionsNoMerge(ctx, w.rt, r, noMergeApproval); !errors.Is(err, store.ErrState) {
				t.Fatal("existing value replaced", err)
			}
		})
	}
}
