package store_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// R5:5.7 batch of cores (landing_batch_grant.go): a batch of core roots
// waits on one card, its lead's; the lead's material is the batch's, and
// its one grant authorizes every member at the batch gate.

// cores is a batch of core roots prepared on M0 commitA with R commitB, its
// lead's CI pinned and green, grants enabled.
type cores struct {
	w          *world
	roots, ops []string
	at         time.Time
	src        store.LandingSource
	gsrc       store.GrantSources
	ci         landing.CIRun

	mu           sync.Mutex
	lines        map[string]int  // root -> lines its result adds
	presentation map[string]bool // root -> an additional display check failure
}

// coresBatch is n roots each changing its own core path, root i adding
// lines[i] lines, claimed and recorded as one batch, every branch pushed.
func coresBatch(t *testing.T, lines ...int) *cores {
	t.Helper()
	ctx := ctxOf(t)
	w, roots, at := batchRoots(t, len(lines))
	c := &cores{w: w, roots: roots, at: at, lines: map[string]int{}, presentation: map[string]bool{}}
	for i, root := range roots {
		c.lines[root] = lines[i]
	}
	freezeClock(t, at.Add(-35*time.Minute))
	enableGrants(t, w, signer{t, rptest.NewKey(t)}, 1, true)
	c.src = store.LandingSource{
		Result: func(_ context.Context, _, task, _, _, _ string) (bool, []rules.LandingChange, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			path := fmt.Sprintf("api/internal/agentrt/store/cores_%s.go", strings.ToLower(task[len(task)-6:]))
			changes := []rules.LandingChange{{OldPath: path, NewPath: path, OldMode: "100644", NewMode: "100644", Added: c.lines[task], Deleted: 1, AddedText: "x"}}
			if c.presentation[task] {
				changes = append(changes, rules.LandingChange{NewPath: "console/src/components/Batch.tsx", NewMode: "100644", Added: 1, AddedText: "fetch('/api')"})
			}
			return true, changes, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			return landing.MainHistory{State: landing.Never}, nil
		},
		GrantPatch: func(context.Context, string, string, string) (store.GrantPatch, error) {
			return store.GrantPatch{SHA256: strings.Repeat("7", 64), Shown: "diff --git a/x b/x\n", Size: 19, Match: true}, nil
		},
		GrantCI: func(context.Context, store.GrantCIKey, time.Time) (store.GrantCIEvidence, error) {
			return store.GrantCIEvidence{Pipeline: "bifrost-ci-platform", Run: c.ci.Run, UID: c.ci.UID, CreatedAt: c.ci.CreatedAt, Status: c.ci.Status,
				Candidate: c.ci.PlatformCommit, Siblings: map[string]string{"bifrost-ui": c.ci.UICommit, "bifrost-trade-infra": c.ci.InfraCommit}}, nil
		},
	}
	c.gsrc = store.GrantSources{Facts: c.src, Main: func(context.Context) (string, error) { return commitA, nil }}
	freezeClock(t, at.Add(-30*time.Minute))
	for _, root := range roots {
		op, d, err := store.ClaimLandingInBatch(ctx, w.rt, c.src, "prod", root, at, c.ops)
		ok(t, err)
		if !d.Allowed || op == "" {
			t.Fatal("batch claim", root, d)
		}
		c.ops = append(c.ops, op)
	}
	ok(t, store.AppendLandingBatch(ctx, w.rt, "bifrost-platform", batchOf(c.ops, at.Add(-30*time.Minute))))
	prepared := at.Add(-29 * time.Minute)
	for i, op := range c.ops {
		c4Prepare(t, w, roots[i], op, prepared, true)
	}
	key := store.GrantCIKey{Repo: "bifrost-platform", Revision: commitB, Base: commitA,
		Siblings: map[string]string{"bifrost-ui": commitC, "bifrost-trade-infra": commitA}}
	ok(t, store.AppendCIKick(ctx, w.rt, c.ops[0], "ci_kick", 1, key, "test: pinned kick"))
	c.ci = landing.CIRun{Run: "green", UID: "uid-green", R: commitB, Status: "Succeeded", CreatedAt: prepared.Add(time.Minute),
		PlatformCommit: commitB, UICommit: commitC, InfraCommit: commitA}
	return c
}

func (c *cores) coresSetPresentation(root string, fail bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.presentation[root] = fail
}

func (c *cores) coresGate(t *testing.T) ([]store.Authorization, landing.Decision) {
	t.Helper()
	ctx := ctxOf(t)
	auths, d, err := store.AuthorizeBatchPush(ctx, c.w.rt, c.src, c.ops, func(context.Context, landing.Operation) (store.LiveObservation, error) {
		return store.LiveObservation{Main: commitA, CI: c.ci}, nil
	})
	ok(t, err)
	return auths, d
}

// waiting runs the batch gate before any grant: the lead alone waits.
func (c *cores) coresWaiting(t *testing.T) {
	t.Helper()
	freezeClock(t, c.at.Add(-20*time.Minute))
	if auths, d := c.coresGate(t); d.Allowed || d.Reason != "awaiting_grant" || d.Detail["op"] != c.ops[0] || auths != nil {
		t.Fatalf("the lead does not wait: %+v", d)
	}
	for i, op := range c.ops {
		want := map[bool]string{true: "awaiting_grant", false: "branch_pushed"}[i == 0]
		if p, _ := phaseOf(t, c.w, op); p != want {
			t.Fatal("member", i, p)
		}
	}
	freezeClock(t, c.at)
}

func (c *cores) coresMaterial(t *testing.T) *store.LandingGrantMaterial {
	t.Helper()
	ctx := ctxOf(t)
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.ops[0], false)
	ok(t, err)
	return m
}

// coresApprovals numbers the approvals of the whole run: tests that run at
// once take from it, each number once.
var coresApprovals atomic.Int64

func (c *cores) coresGrant(t *testing.T, m *store.LandingGrantMaterial) store.LandingGrant {
	t.Helper()
	ctx := ctxOf(t)
	n := coresApprovals.Add(1)
	a := store.GrantApproval{ID: fmt.Sprintf("appr_cores_%d", n), Number: 300 + n, Action: m.Action,
		ParamsHash: strings.Repeat("cd", 32), ApprovedAt: c.at.Add(-time.Minute)}
	g, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, a, store.GrantParams{Env: "prod", Repo: "bifrost-platform", Task: c.roots[0], Op: c.ops[0],
		R: commitB, MaterialDigest: m.MaterialDigest})
	ok(t, err)
	return g
}

// intents are the main_intents written, as op: the grant its use names /
// the grant its landing_batch_grant names.
func (c *cores) coresIntents(t *testing.T) []string {
	t.Helper()
	ctx := ctxOf(t)
	rows, err := c.w.rt.Query(ctx, `SELECT body_json->'landing'->>'op', COALESCE(body_json->'landing_grant_use'->>'grant_id',''),
	    COALESCE(body_json->'landing_batch_grant'->>'grant_id','') FROM agentrt.event
	  WHERE event_stream_id = $1 AND body_json->'landing'->>'kind' = 'main_intent' ORDER BY seq`, landingRepoStream)
	ok(t, err)
	var out []string
	for rows.Next() {
		var op, use, batch string
		ok(t, rows.Scan(&op, &use, &batch))
		out = append(out, op+":"+use+"/"+batch)
	}
	ok(t, rows.Err())
	return out
}

// Two core roots, the second's change large:
// the lead's material waives their soft checks without a size category, each
// member's shown, and takes approve_core_landing; the one grant on it names
// the batch, and at the gate authorizes both members, the lead consuming it
// and the other's main_intent naming it.
func TestBatchCoresOneGrantAuthorizesTheBatch(t *testing.T) {
	t.Parallel()
	ownClock(t)
	c := coresBatch(t, 1, 10000)
	c.coresWaiting(t)
	m := c.coresMaterial(t)
	union := []string{"core", "rule_paths"}
	if len(m.Problems) != 0 || m.Action != store.ActionCoreLanding || !slices.Equal(m.SoftAtRequest, union) || m.Batch == nil {
		t.Fatalf("material: %v %s %v %+v", m.Problems, m.Action, m.SoftAtRequest, m.Batch)
	}
	if b := m.Batch; len(b.Members) != 2 || b.Members[0].Op != c.ops[0] || !slices.Equal(b.Members[0].Soft, []string{"core", "rule_paths"}) ||
		b.Members[1].Op != c.ops[1] || !slices.Equal(b.Members[1].Soft, union) {
		t.Fatalf("members: %+v", b.Members)
	}
	g := c.coresGrant(t, m)
	if !slices.Equal(g.Waives, union) || g.Batch == nil || g.Batch.Batch != landing.BatchID(c.ops[0]) || !slices.Equal(g.Batch.Members, c.ops) {
		t.Fatalf("grant: %v %+v", g.Waives, g.Batch)
	}
	auths, d := c.coresGate(t)
	if !d.Allowed || len(auths) != 2 || auths[0].Op != c.ops[0] || auths[1].Op != c.ops[1] || auths[0].R != auths[1].R {
		t.Fatalf("gate: %+v %+v", d, auths)
	}
	if got := c.coresIntents(t); !slices.Equal(got, []string{c.ops[0] + ":" + g.GrantID + "/", c.ops[1] + ":/" + g.GrantID}) {
		t.Fatal("intents", got)
	}
	for _, op := range c.ops {
		if p, _ := phaseOf(t, c.w, op); p != "main_intent" {
			t.Fatal("no intent", op, p)
		}
	}
}

// The STG gate reads the batch as the landing
// gate wrote it: the lead used the grant, the other member rests on it, and
// the member carries that grant and the batch it names (M-374, 2026-11-10:
// without it B5 refused the batch as "rule").
func TestBatchCoresSTGGateReadsTheMemberGrant(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	c := coresBatch(t, 1, 400)
	c.coresWaiting(t)
	g := c.coresGrant(t, c.coresMaterial(t))
	if _, d := c.coresGate(t); !d.Allowed {
		t.Fatal("gate", d)
	}
	used, batch, id, has, members, err := store.StgGateAuthorityForTest(ctx, c.w.rt, c.ops[0], c.roots, c.at)
	ok(t, err)
	if !used || batch != "" {
		t.Fatal("lead", used, batch)
	}
	used, batch, id, has, members, err = store.StgGateAuthorityForTest(ctx, c.w.rt, c.ops[1], c.roots, c.at)
	ok(t, err)
	if used || batch != g.GrantID || !has || id != g.GrantID || !slices.Equal(members, c.ops) {
		t.Fatal("member", used, batch, has, id, members)
	}
}

// A grant that does not waive one of a member's
// soft checks authorizes nothing: the member is refused awaiting_grant,
// naming it, nothing of the batch is written, and the grant stays usable.
// The member's new check also changes the lead's material: another card.
func TestBatchCoresGateRefusesAMemberTheGrantDoesNotWaive(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	c := coresBatch(t, 1, 1)
	c.coresWaiting(t)
	m := c.coresMaterial(t)
	if !slices.Equal(m.SoftAtRequest, []string{"core", "rule_paths"}) || len(m.Problems) != 0 {
		t.Fatalf("material: %v %v", m.SoftAtRequest, m.Problems)
	}
	c.coresGrant(t, m)
	c.coresSetPresentation(c.roots[1], true)
	if again := c.coresMaterial(t); again.MaterialDigest == m.MaterialDigest || !slices.Contains(again.SoftAtRequest, "presentation") {
		t.Fatal("the member's new check is the same card", again.SoftAtRequest)
	}
	auths, d := c.coresGate(t)
	if d.Allowed || d.Reason != "awaiting_grant" || d.Detail["op"] != c.ops[1] || auths != nil {
		t.Fatalf("gate: %+v", d)
	}
	if got := c.coresIntents(t); len(got) != 0 {
		t.Fatal("intents on a refusal", got)
	}
	if usable, err := store.HasUsableLandingGrant(ctx, c.w.rt, c.ops[0]); err != nil || !usable {
		t.Fatal("the grant was spent on a refusal", err)
	}
	// Back to what the grant waives: the batch lands on it.
	c.coresSetPresentation(c.roots[1], false)
	if _, d := c.coresGate(t); !d.Allowed {
		t.Fatalf("gate: %+v", d)
	}
}

// A grant whose batch is another (other
// members) is the lead's alone: the member is refused, nothing written.
func TestBatchCoresGateRefusesTheGrantOfAnotherBatch(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	c := coresBatch(t, 1, 1)
	c.coresWaiting(t)
	m := c.coresMaterial(t)
	other := "land:t_" + strings.Repeat("Z", 26) + ":1"
	g := store.LandingGrant{V: "1", GrantID: "lg_cores_other", ApprovalID: "cores_other", ApprovalNumber: 7, Action: m.Action,
		ParamsHash: strings.Repeat("ab", 32), Home: "prod", Env: "prod", Tenant: "dev", Repo: "bifrost-platform", Task: c.roots[0], Attempt: m.Candidate.Attempt,
		Op: c.ops[0], A: m.Candidate.A, Base: m.Candidate.Base, R: commitB, M0: commitA, CommitterDate: m.Candidate.CommitterDate,
		CandidateDigest: m.CandidateDigest, MaterialDigest: m.MaterialDigest, Waives: m.SoftAtRequest, SoftAtRequest: m.SoftAtRequest,
		Files: 1, Lines: 2, Rule: m.Rule, ChecksVersion: m.ChecksVersion,
		CI:      store.GrantCI{Pipeline: "bifrost-ci-platform", Run: "green", UID: "uid-green", CreatedAt: m.Candidate.CI.CreatedAt, Status: "Succeeded", SHA: commitB},
		Purpose: "landing_main", ApprovedAt: c.at.Add(-time.Minute).Format(time.RFC3339Nano), GrantedAt: c.at.Format(time.RFC3339Nano),
		ExpiresAt: c.at.Add(-time.Minute + store.LandingGrantTTL).Format(time.RFC3339Nano),
		Batch:     &store.LandingGrantBatch{Batch: landing.BatchID(c.ops[0]), Members: []string{c.ops[0], c.ops[1], other}}}
	ok(t, store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: c.roots[0],
			Body: map[string]any{"text": "a grant of another batch", "landing_grant": g.Body()}})
		return err
	}))
	auths, d := c.coresGate(t)
	if d.Allowed || d.Reason != "awaiting_grant" || d.Detail["op"] != c.ops[1] || auths != nil {
		t.Fatalf("gate: %+v", d)
	}
	if got := c.coresIntents(t); len(got) != 0 {
		t.Fatal("intents on a refusal", got)
	}
}
