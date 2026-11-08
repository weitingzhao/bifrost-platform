package store_test

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"sync"
	"testing"
	"time"
)

func TestRecoverBatchPushKeepsGreenCIAndOriginalGrant(t *testing.T) { runRecoveryTest(t, false) }
func TestRecoverBatchPushAfterBoundKeepsGreenCI(t *testing.T)       { runRecoveryTest(t, true) }
func runRecoveryTest(t *testing.T, bound bool) {
	c := coresBatch(t, 10, 10)
	c.coresWaiting(t)
	g := c.coresGrant(t, c.coresMaterial(t))
	auths, d := c.coresGate(t)
	if !d.Allowed {
		t.Fatal(d)
	}
	for _, a := range auths {
		_, err := store.RecordPushResult(ctx, c.w.rt, a, "unknown", "agentrt-lander")
		ok(t, err)
	}
	p := store.PushRecoveryParams{Env: "prod", Repo: "bifrost-platform", Op: c.ops[0], R: commitB, M0: commitA, GrantID: g.GrantID, CIRun: g.CI.Run}
	if err := store.RecoverBatchPush(ctx, c.w.rt, c.gsrc, p, "appr_recovery"); err == nil {
		t.Fatal("recovered inside push fence")
	}
	store.FreezeEventClock(c.at.Add(store.MainFence + time.Minute))
	if bound {
		for _, op := range c.ops {
			ok(t, appendLanding(c.w, "landing:bifrost-platform", map[string]any{"v": landing.Version, "kind": "failed", "op": op, "why": "bound"}))
		}
	}
	bad := p
	bad.R = commitA
	if err := store.RecoverBatchPush(ctx, c.w.rt, c.gsrc, bad, "appr_recovery"); err == nil {
		t.Fatal("accepted changed candidate")
	}
	moved := c.gsrc
	moved.Main = func(context.Context) (string, error) { return commitB, nil }
	if err := store.RecoverBatchPush(ctx, c.w.rt, moved, p, "appr_recovery"); err == nil {
		t.Fatal("accepted moved main")
	}
	red := c.gsrc
	red.Facts = c.src
	red.Facts.GrantCI = func(context.Context, store.GrantCIKey, time.Time) (store.GrantCIEvidence, error) {
		return store.GrantCIEvidence{Status: "Failed"}, nil
	}
	if err := store.RecoverBatchPush(ctx, c.w.rt, red, p, "appr_recovery"); err == nil {
		t.Fatal("accepted red CI")
	}
	store.FreezeEventClock(c.at.Add(25 * time.Hour))
	if err := store.RecoverBatchPush(ctx, c.w.rt, c.gsrc, p, "appr_recovery"); err == nil {
		t.Fatal("expired original grant accepted")
	}
	store.FreezeEventClock(c.at.Add(store.MainFence + time.Minute))
	uid := c.ci.UID
	c.ci.UID = "different-run-object"
	if err := store.RecoverBatchPush(ctx, c.w.rt, c.gsrc, p, "appr_recovery"); err == nil {
		t.Fatal("replacement CI object accepted")
	}
	c.ci.UID = uid
	ok(t, store.RecoverBatchPush(ctx, c.w.rt, c.gsrc, p, ""))
	if phase, _ := phaseOf(t, c.w, c.ops[0]); phase != map[bool]string{false: "main_result", true: "failed"}[bound] {
		t.Fatal("preflight mutated state", phase)
	}
	pool, err := pgxpool.New(ctx, c.w.rt.Config().ConnString())
	ok(t, err)
	defer pool.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- store.RecoverBatchPush(ctx, pool, c.gsrc, p, "appr_recovery") }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		ok(t, err)
	}
	var n int
	ok(t, c.w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE body_json->'landing'->>'kind'='push_recovered'`).Scan(&n))
	if n != 2 {
		t.Fatal("duplicate recovery records", n)
	}
	if err := store.RecoverBatchPush(ctx, c.w.rt, c.gsrc, p, "appr_second"); err == nil {
		t.Fatal("second recovery accepted")
	}
	auths, d = c.coresGate(t)
	if !d.Allowed || len(auths) != 2 {
		t.Fatalf("same green batch did not pass final gate: %+v", d)
	}
	for _, a := range auths {
		if a.R != commitB || a.M0 != commitA {
			t.Fatal("candidate changed")
		}
	}
}

func TestRecoveryRecordCannotBeInjected(t *testing.T) {
	c := coresBatch(t, 10, 10)
	err := store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "forged", "landing": map[string]any{"v": landing.Version, "kind": "push_recovered", "op": c.ops[0], "R": commitB, "M0": commitA, "grant_id": "lg_fake", "approval_id": "appr_fake", "resume": "awaiting_grant"}}})
		return err
	})
	if err == nil {
		t.Fatal("injected recovery accepted")
	}
}
