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

const b5Batch = "stg:bifrost-platform:1"

type b5Fixture struct {
	w        *world
	root, op string
	at       time.Time
	obs      store.StgGateObservation
	src      store.StgGateSource
}

func b5Append(t *testing.T, w *world, r map[string]any) store.Appended {
	t.Helper()
	var a store.Appended
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		a, err = store.Append(ctx, tx, store.Event{Stream: store.StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "B5-c fixture", "stg_release": r}})
		return err
	}))
	return a
}
func b5Record(kind, scope string, kv ...any) map[string]any {
	r := map[string]any{"v": "1", "kind": kind, "scope": scope}
	if scope == "batch" {
		r["batch"] = b5Batch
	}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}
func b5Ready(t *testing.T) *b5Fixture {
	t.Helper()
	w, root, op, src, prepared := liveReady(t)
	at := prepared.Add(10 * time.Minute)
	t.Cleanup(store.FreezeEventClock(at))
	b5AssertRule(t, w, at, 1, "bifrost-deliver-platform")
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
	ok(t, err)
	if !d.Allowed {
		t.Fatal("landing prerequisite", d)
	}
	_, err = store.RecordPushResult(ctx, w.rt, auth, "ok")
	ok(t, err)
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "landed", "R", commitB)))
	return b5Snapshot(t, w, root, op, at)
}

func b5AssertRule(t *testing.T, w *world, at time.Time, version int, release string) {
	t.Helper()
	r := inForce(t, w, "landing:dev", at)
	if r == nil || r.Landing == nil {
		t.Fatalf("B5 fixture: no landing rule at %s: %+v", at.Format(time.RFC3339Nano), r)
	}
	repo, found := r.Landing.Repo("bifrost-platform")
	t.Logf("B5 fixture: rule=%s version=%d signed=%t stg_release=%q", r.ID, r.Version, r.SignedBy != "", repo.STGRelease)
	if !found || r.Version != version || repo.STGRelease != release || release != "none" && r.SignedBy == "" {
		t.Fatalf("B5 fixture: want rule version=%d stg_release=%q (signature required when enabled)", version, release)
	}
}

func b5Snapshot(t *testing.T, w *world, root, op string, at time.Time) *b5Fixture {
	t.Helper()
	x := &b5Fixture{w: w, root: root, op: op, at: at}
	api, console := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("c", 64)
	workloads := map[string]any{}
	for _, name := range []string{"platform-api", "platform-workers", "platform-console"} {
		d := api
		if name == "platform-console" {
			d = console
		}
		workloads[name] = map[string]any{"uid": name + "-uid", "generation": int64(1), "digest": d}
	}
	base := map[string]any{"P": commitA, "api_digest": api, "console_digest": console, "evidence": "cluster/cicd/base"}
	b5Append(t, w, b5Record("evidence", "ext", "evidence", "cluster/cicd/base", "source", "external", "run", "base-run", "P", commitA, "U", commitA, "infra", commitA, "content", map[string]any{"api_digest": api, "console_digest": console}, "deployed", map[string]any{"pin_sync_ok": true, "workloads": workloads, "at": at.Format(time.RFC3339Nano)}, "health", map[string]any{"status": "ok"}))
	m := map[string]any{"root": root, "op": op, "R": commitB}
	b5Append(t, w, b5Record("opened", "batch", "members", []any{map[string]any{"root": root, "op": op, "R": commitB, "gen": 1}}, "first_pending_at", map[string]any{commitB: at.Format(time.RFC3339Nano)}))
	b5Append(t, w, b5Record("p_selected", "batch", "P", commitB, "at", at.Format(time.RFC3339Nano)))
	ci := map[string]any{"name": "ci-release", "uid": "ci-release-uid", "created_at": at.Add(-time.Minute).Format(time.RFC3339Nano), "platform_commit": commitB, "ui_commit": commitA, "infra_commit": commitA, "pipeline_ref": "bifrost-ci-platform", "repo_url": landing.LandingRepos["bifrost-platform"].Remote, "read_at": at.Format(time.RFC3339Nano)}
	b5Append(t, w, b5Record("ci_accepted", "batch", "P", commitB, "ci", ci))
	inputs := map[string]any{"namespace": "cicd", "pipeline": "bifrost-deliver-platform", "revision": commitB, "uiRevision": commitA, "infraRevision": commitA, "params_canonical": map[string]any{"revision": commitB, "uiRevision": commitA, "infraRevision": commitA}}
	b5Append(t, w, b5Record("snapshot", "batch", "try", 1, "inputs", inputs, "base", base, "set", []any{m}, "not_before", at.Format(time.RFC3339Nano)))
	x.obs = store.StgGateObservation{Main: commitB, BaseP: commitA, ReadAt: at, Workloads: workloads, CI: store.StgCIObservation{Status: "Succeeded", UID: "ci-release-uid", ReadAt: at, Accepted: ci}, Lease: store.StgLeaseObservation{Who: "stgrel-bifrost-platform-1-1", What: "bifrost-platform", Env: "stg", ResourceVersion: "10", Generation: 1, Expires: at.Add(5 * time.Minute)}, Commits: []landing.ReleaseCommit{{SHA: commitB, Parents: []string{commitA}, Message: "Result\n\nAgentrt-Task: " + root + "\nLanded-By: agentrt-prod/" + op + "\n", Changes: []rules.LandingChange{{OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p>Ready</p>"}}}}}
	x.src = store.StgGateSource{Environment: "prod", Live: func() bool { return true }, Generation: func() int64 { return 1 }, Observe: func(context.Context, store.StgRecord) (store.StgGateObservation, error) { return x.obs, nil }}
	return x
}
func (x *b5Fixture) read(t *testing.T) store.StgState {
	t.Helper()
	s, err := store.ReadStgRelease(ctx, x.w.rt)
	ok(t, err)
	return s
}
func (x *b5Fixture) permit(t *testing.T) store.StgSendPermit {
	t.Helper()
	p, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
	ok(t, err)
	if !d.Allowed || p.IntentSeq == 0 {
		t.Fatal("same-entry positive control", p, d)
	}
	return p
}
func (x *b5Fixture) advance(at time.Time) {
	store.FreezeEventClock(at)
	x.obs.ReadAt = at
	x.obs.CI.ReadAt = at
	x.obs.CI.Accepted["read_at"] = at.Format(time.RFC3339Nano)
	x.obs.Lease.Expires = at.Add(5 * time.Minute)
}
func (x *b5Fixture) none(t *testing.T) {
	t.Helper()
	body := strings.Replace(landingRuleBody, "stg_release: bifrost-deliver-platform", "stg_release: none", 1)
	_, err := store.TightenRule(ctx, x.w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", since, until, body)))
	ok(t, err)
	b5AssertRule(t, x.w, store.DecisionAt(), 2, "none")
}

// Serial: FreezeEventClock and real PostgreSQL transactions.
func TestStgGatePGFirstPermitAndDuplicate(t *testing.T) {
	x := b5Ready(t)
	p := x.permit(t)
	s := x.read(t)
	if s.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 1 || p.IntentSeq != s.Batches[b5Batch].Tries[1].Started.Seq || !p.Deadline.Equal(x.at.Add(2*time.Minute)) {
		t.Fatal("atomic permit/debit", p, s.Budget)
	}
	current, err := store.StgPermitCurrent(ctx, x.w.rt, p, 1, x.at)
	ok(t, err)
	if !current {
		t.Fatal("committed permit not current")
	}
	for _, test := range []struct {
		gen int64
		now time.Time
	}{{2, x.at}, {1, p.Deadline}} {
		current, err = store.StgPermitCurrent(ctx, x.w.rt, p, test.gen, test.now)
		ok(t, err)
		if current {
			t.Fatal("stale permit current")
		}
	}
	_, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
	ok(t, err)
	if d.Allowed || d.Why != "not_ready" || x.read(t).LastSeq != s.LastSeq {
		t.Fatal("duplicate intent", d)
	}
}

// Serial: FreezeEventClock. none disables sends but keeps facts recordable.
func TestStgGatePGNoneRecoveryAndBudget(t *testing.T) {
	for _, mode := range []string{"resend", "unsent", "observe under none", "none first"} {
		t.Run(mode, func(t *testing.T) {
			x := b5Ready(t)
			if mode == "none first" {
				x.none(t)
				for i := 0; i < 3; i++ {
					_, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
					ok(t, err)
					if d.Allowed || d.Why != "rule" {
						t.Fatal(d)
					}
				}
				s := x.read(t)
				if len(s.Blocks) != 1 || len(s.Budget) != 0 || s.Batches[b5Batch].Tries[1].Started.Seq != 0 {
					t.Fatal("none created/debited or duplicated block", s.Budget, len(s.Blocks))
				}
				return
			}
			p := x.permit(t)
			at := p.Deadline.Add(time.Second)
			x.advance(at)
			if mode != "resend" {
				x.none(t)
			}
			if mode == "observe under none" {
				before := x.read(t).LastSeq
				action, err := store.RecordStgRecovery(ctx, x.w.rt, b5Batch, 1, store.StgRunObservation{Kind: "unknown", IntentSeq: p.IntentSeq, PermitSeq: p.PermitSeq, ReadAt: at})
				ok(t, err)
				if action != "wait" || x.read(t).LastSeq != before {
					t.Fatal("unknown concluded")
				}
				action, err = store.RecordStgRecovery(ctx, x.w.rt, b5Batch, 1, store.StgRunObservation{Kind: "found", UID: "run-under-none", IntentSeq: p.IntentSeq, PermitSeq: p.PermitSeq, ReadAt: at})
				ok(t, err)
				if action != "bound" || x.read(t).Batches[b5Batch].Tries[1].Bound.Record.String("uid") != "run-under-none" {
					t.Fatal(action)
				}
				return
			}
			proof := &store.StgNotFoundProof{IntentSeq: p.IntentSeq, PermitSeq: p.PermitSeq, ReadAt: at, FinalizerGuaranteed: true}
			q, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, proof)
			ok(t, err)
			s := x.read(t)
			tr := s.Batches[b5Batch].Tries[1]
			if s.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 1 {
				t.Fatal("resend debited or unsent refunded")
			}
			if mode == "resend" {
				if !d.Allowed || q.IntentSeq != p.IntentSeq || q.PermitSeq == p.PermitSeq || tr.Permit.Record.String("kind") != "resend" {
					t.Fatal(q, d)
				}
			} else if d.Allowed || d.Why != "rule" || tr.Unsent.Seq == 0 || tr.Occupied() {
				t.Fatal("none did not end proven unsent", d, tr)
			}
		})
	}
}

// Serial: FreezeEventClock. Slow external reads hold no database locks; a
// signed tightening during them is selected only at the final DecisionAt.
func TestStgGatePGRuleChangesDuringSlowRead(t *testing.T) {
	x := b5Ready(t)
	x.src.Observe = func(context.Context, store.StgRecord) (store.StgGateObservation, error) { x.none(t); return x.obs, nil }
	_, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
	ok(t, err)
	if d.Allowed || d.Why != "rule" || len(x.read(t).Budget) != 0 {
		t.Fatal(d)
	}
}

// Serial: SetHook changes the binding after discovery, before locks. No
// record (including a block) may survive the transaction's required rollback.
func TestStgGatePGBindingChangeRollsBack(t *testing.T) {
	x := b5Ready(t)
	before := x.read(t).LastSeq
	defer store.SetHook(func(point, _ string) {
		if point == "stg gate: bindings read" {
			tamperAdmin(t, x.w, `UPDATE agentrt.task SET mission_id='M-1' WHERE task_id=$1`, x.root)
		}
	})()
	_, _, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
	if !errors.Is(err, store.ErrLandingBindingChanged) || x.read(t).LastSeq != before {
		t.Fatal("rebind not rolled back", err)
	}
}

// Serial: FreezeEventClock/SetHook; PostgreSQL confirms the second connection
// waits on the first before it can attempt a second debit.
func TestStgGatePGTwoConnectionsOnePermit(t *testing.T) {
	x := b5Ready(t)
	second := x.w.db.Connect(t, x.w.db.Runtime)
	observer := x.w.db.Connect(t, x.w.db.Runtime)
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	held, release := make(chan struct{}), make(chan struct{})
	defer store.SetHook(func(point, _ string) {
		if point == "stg gate: before permit" {
			close(held)
			select {
			case <-release:
			case <-c.Done():
			}
		}
	})()
	type result struct {
		p   store.StgSendPermit
		d   store.StgGateDecision
		err error
	}
	one, two := make(chan result, 1), make(chan result, 1)
	go func() {
		p, d, e := store.AuthorizeStgRelease(c, x.w.rt, b5Batch, 1, x.src, nil)
		one <- result{p, d, e}
	}()
	select {
	case <-held:
	case got := <-one:
		t.Fatalf("first gate returned before permit: permit=%+v decision=%+v err=%v", got.p, got.d, got.err)
	case <-c.Done():
		t.Fatal("first gate did not reach permit")
	}
	go func() {
		p, d, e := store.AuthorizeStgRelease(c, second, b5Batch, 1, x.src, nil)
		two <- result{p, d, e}
	}()
	for {
		var waits bool
		err := observer.QueryRow(c, `SELECT $2::int=ANY(pg_blocking_pids($1::int))`, second.PgConn().PID(), x.w.rt.PgConn().PID()).Scan(&waits)
		if err != nil {
			close(release)
			t.Fatal(err)
		}
		if waits {
			break
		}
		select {
		case <-c.Done():
			close(release)
			t.Fatal("second did not wait")
		case <-time.After(10 * time.Millisecond):
		}
	}
	close(release)
	a, b := <-one, <-two
	ok(t, a.err)
	ok(t, b.err)
	if !a.d.Allowed || b.d.Allowed || b.d.Why != "not_ready" || x.read(t).Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 1 {
		t.Fatal(a, b)
	}
}

// Serial: the C-5 fixture consumes a real recorded grant before B5 reads it.
// The current rule does not allow core changes; only the grant's STG flag
// authorizes this commit, and the B5 debit never changes that grant record.
func TestStgGatePGConsumedCoreGrant(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	g := c.grant(t, store.ActionCoreLanding)
	auth, d := c.gate(t)
	if !d.Allowed {
		t.Fatal(d)
	}
	_, err := store.RecordPushResult(ctx, c.w.rt, auth, "ok")
	ok(t, err)
	ok(t, appendLanding(c.w, landingRepoStream, newLandingRecord(c.op, "landed", "R", commitB)))
	ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "loosen", since, until, landingRuleBody))))
	b5AssertRule(t, c.w, store.DecisionAt(), 2, "bifrost-deliver-platform")
	x := b5Snapshot(t, c.w, c.root, c.op, store.DecisionAt())
	x.obs.Commits[0].Changes[0].OldPath = c5Core
	x.obs.Commits[0].Changes[0].NewPath = c5Core
	p := x.permit(t)
	if p.IntentSeq == 0 {
		t.Fatal("grant did not permit")
	}
	ok(t, store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
		ev, found, err := store.LandingGrantEvidence(ctx, tx, "bifrost-platform", c.op)
		if err == nil && (!found || ev.GrantID != g.GrantID || !ev.STGRelease) {
			t.Fatal("B5 changed landing grant", ev, found)
		}
		return err
	}))
}
