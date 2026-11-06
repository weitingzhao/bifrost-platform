package store_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Infra's floor grant (approve_floor_landing): a k8s/** change waits for the
// Owner's grant instead of being refused; the grant pins M0, R and each
// file's bytes; the final gate recomputes M0..R from Git just before the
// push, spends the grant on any difference and holds it while main, CI or
// the window are not ready.

const floorManifest = "k8s/apps/web/deploy.yaml"

var floorSHA = strings.Repeat("5", 64)

// floorCase is a waiting infra operation of a floor-grantable change, with
// what its grant and its final gate read.
type floorCase struct {
	w        *world
	root, op string
	src      store.LandingSource
	gsrc     store.GrantSources
	at       time.Time
	tree     landing.FloorTree
	ci       landing.CIRun
	main     string
	window   bool
}

// enableInfraGrants signs the dev envelope naming the coordinator's external
// host for both repositories: grants are enabled for infra (§2.1).
func enableInfraGrants(t *testing.T, w *world) {
	t.Helper()
	registerSeat(t, w)
	ok(t, store.RegisterHost(ctx, w.rt, "owner", externalHost, store.HostExternal))
	body := strings.Replace(pinEnvelope(1, rules.Initial, true), "repos: [bifrost-platform]", "repos: [bifrost-platform, bifrost-trade-infra]", 1)
	body = strings.Replace(body, "hosts: [h1, coordinator-mbp]", "hosts: [h1, h2, h3, h4, h5, h6, h7, h8, coordinator-mbp]", 1)
	ok(t, firstErr(signer{t, rptest.NewKey(t)}.sign(w, body)))
}

func floorChange() rules.LandingChange {
	return rules.LandingChange{Kind: "M", OldPath: floorManifest, NewPath: floorManifest, OldMode: "100644", NewMode: "100644", Added: 3, Deleted: 1,
		AddedText: "kind: Deployment\nreplicas: 2\nimage: web:2\n"}
}

// floorWait claims, prepares and gates the change to awaiting_grant.
func floorWait(t *testing.T) *floorCase {
	t.Helper()
	w, root, src := infraWorld(t, []rules.LandingChange{floorChange()})
	enableInfraGrants(t, w)
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, time.Now())
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatalf("a floor-grantable change was refused: %+v", d)
	}
	c := &floorCase{w: w, root: root, op: op, src: src, main: commitA, at: time.Now().UTC().Truncate(time.Second)}
	t.Cleanup(store.FreezeEventClock(c.at))
	prepareS6(t, w, infraRepo, root, op, c.at)
	c.ci = landing.CIRun{Run: "landing-ci-infra-green-1", UID: "uid-infra", R: commitB, Status: "Succeeded", CreatedAt: c.at.Add(time.Minute),
		Results: map[string]string{"infra-commit": commitB}}
	c.tree = landing.FloorTree{OnM0: true, Files: []landing.FloorFile{{Path: floorManifest, Kind: "M", Mode: "100644", SHA256: floorSHA}}}
	c.src.GrantPatch = func(context.Context, string, string, string) (store.GrantPatch, error) {
		return store.GrantPatch{SHA256: strings.Repeat("7", 64), Shown: "diff --git a/" + floorManifest + "\n", Size: 40, Match: true}, nil
	}
	c.src.GrantCI = func(context.Context, store.GrantCIKey, time.Time) (store.GrantCIEvidence, error) {
		t.Fatal("a floor grant read a pinned platform CI run")
		return store.GrantCIEvidence{}, nil
	}
	c.src.FloorFiles = func(_ context.Context, repo, m0, r string) (landing.FloorTree, error) {
		if repo != infraRepo || m0 != commitA || r != commitB {
			t.Fatalf("floor files of %s %s..%s", repo, m0, r)
		}
		return c.tree, nil
	}
	store.FreezeEventClock(c.at.Add(2 * time.Minute))
	if _, d := c.gate(t); d.Reason != "awaiting_grant" {
		t.Fatalf("not waiting: %+v", d)
	}
	if p := landingState(t, w, infraStream).Operations[op].Phase; p != "awaiting_grant" {
		t.Fatal("phase", p)
	}
	c.gsrc = store.GrantSources{Facts: c.src, Main: func(context.Context) (string, error) { return c.main, nil }}
	store.FreezeEventClock(c.at.Add(3 * time.Minute))
	return c
}

func (c *floorCase) observe() func(context.Context, landing.Operation) (store.LiveObservation, error) {
	return func(context.Context, landing.Operation) (store.LiveObservation, error) {
		return store.LiveObservation{Main: c.main, CI: c.ci, WindowBlocked: c.window}, nil
	}
}

func (c *floorCase) gate(t *testing.T) (store.Authorization, landing.Decision) {
	t.Helper()
	auth, d, err := store.AuthorizeLandingPush(ctx, c.w.rt, c.src, c.op, c.observe(), infraRepo)
	ok(t, err)
	return auth, d
}

func (c *floorCase) material(t *testing.T) *store.LandingGrantMaterial {
	t.Helper()
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, false)
	ok(t, err)
	return m
}

var floorApprovals = 0

func (c *floorCase) approval(action string) store.GrantApproval {
	floorApprovals++
	return store.GrantApproval{ID: "appr_floor_" + strings.Repeat("y", floorApprovals), Number: int64(500 + floorApprovals), Action: action,
		ParamsHash: strings.Repeat("cd", 32), ApprovedAt: c.at.Add(2 * time.Minute)}
}

func (c *floorCase) params(t *testing.T) store.GrantParams {
	t.Helper()
	return store.GrantParams{Env: "prod", Repo: infraRepo, Task: c.root, Op: c.op, R: commitB, MaterialDigest: c.material(t).MaterialDigest}
}

func (c *floorCase) grant(t *testing.T) store.LandingGrant {
	t.Helper()
	g, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionFloorLanding), c.params(t))
	ok(t, err)
	return g
}

func (c *floorCase) count(t *testing.T, where string) int {
	t.Helper()
	return c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND `+where, infraStream)
}

func (c *floorCase) usable(t *testing.T) bool {
	t.Helper()
	u, err := store.HasUsableLandingGrant(ctx, c.w.rt, c.op)
	ok(t, err)
	return u
}

// Serial: FreezeEventClock and the signing anchor. (a), (c) and (e): the
// k8s change waits; the material pins floor_paths; the grant pushes R with
// landing_grant_use, once; the push result names the pusher.
func TestFloorGrantPushesExactlyThePinnedFiles(t *testing.T) {
	c := floorWait(t)
	m := c.material(t)
	if len(m.Problems) != 0 || m.Action != store.ActionFloorLanding || !slices.Equal(m.SoftAtRequest, []string{"floor_grant", "rule_paths"}) ||
		len(m.FloorPaths) != 1 || m.FloorPaths[0] != (store.GrantFloorPath{Path: floorManifest, Entry: "k8s/**", BlobSHA256: floorSHA}) ||
		m.Title != "Infra floor change" || m.STGNotice != "" || len(m.Candidate.HardCorePaths) != 0 {
		t.Fatalf("material: %+v", m)
	}
	// The pinned files are in material_digest: other bytes, another digest.
	c.tree.Files[0].SHA256 = strings.Repeat("6", 64)
	if other := c.material(t); other.MaterialDigest == m.MaterialDigest || other.CandidateDigest != m.CandidateDigest {
		t.Fatal("floor_paths are not in material_digest alone")
	}
	c.tree.Files[0].SHA256 = floorSHA
	g := c.grant(t)
	if g.Action != store.ActionFloorLanding || g.Repo != infraRepo || !slices.Equal(g.Waives, []string{"floor_grant", "rule_paths"}) ||
		!slices.Equal(g.FloorPaths, m.FloorPaths) || g.MaterialDigest != m.MaterialDigest || g.CI.Pipeline != "bifrost-ci-infra" || g.CI.Run != c.ci.Run || g.CI.SHA != commitB {
		t.Fatalf("grant: %+v", g)
	}
	if c.count(t, `body_json ? 'landing_grant'`) != 1 || c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant'`, landingRepoStream) != 0 {
		t.Fatal("the grant is not on landing:bifrost-trade-infra alone")
	}
	if st, err := store.ReadLandingWait(ctx, c.w.rt, c.op); err != nil || st == nil || len(st.Grants) != 1 || !st.Grants[0].Valid {
		t.Fatalf("status: %+v %v", st, err)
	}
	if !c.usable(t) {
		t.Fatal("grant not usable")
	}
	auth, d := c.gate(t)
	if !d.Allowed || auth.Repo != infraRepo || auth.R != commitB || auth.M0 != commitA {
		t.Fatalf("gate: %+v %+v", d, auth)
	}
	var use string
	ok(t, c.w.rt.QueryRow(ctx, `SELECT body_json->'landing_grant_use'->>'grant_id' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent'`,
		infraStream).Scan(&use))
	if use != g.GrantID || c.usable(t) {
		t.Fatal("consumption", use)
	}
	if _, d = c.gate(t); d.Allowed {
		t.Fatal("authorized twice")
	}
	wrote, err := store.RecordPushResult(ctx, c.w.rt, auth, "ok", "agentrt-lander")
	ok(t, err)
	var pusher, outcome string
	ok(t, c.w.rt.QueryRow(ctx, `SELECT body_json->>'landing_pusher', body_json->'landing'->>'outcome' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_result'`,
		infraStream).Scan(&pusher, &outcome))
	if !wrote || pusher != "agentrt-lander" || outcome != "ok" {
		t.Fatal("push result", wrote, pusher, outcome)
	}
}

// Serial: FreezeEventClock and the signing anchor. (c): a changed byte, an
// extra file, a missing file, a never-grantable path, a Secret, or R not on
// M0 spend the grant and nothing is pushed; a moved main, red or unknown CI
// and a held window leave it usable and push nothing either.
func TestFloorGrantFinalGate(t *testing.T) {
	stale := map[string]func(*floorCase){
		"changed byte": func(c *floorCase) { c.tree.Files[0].SHA256 = strings.Repeat("6", 64) },
		"extra file": func(c *floorCase) {
			c.tree.Files = append(c.tree.Files, landing.FloorFile{Path: "k8s/apps/web/extra.yaml", Kind: "A", Mode: "100644", SHA256: floorSHA})
		},
		"missing file": func(c *floorCase) { c.tree.Files = nil },
		"never-grantable path": func(c *floorCase) {
			c.tree.Files = append(c.tree.Files, landing.FloorFile{Path: ".github/workflows/x.yml", Kind: "A", Mode: "100644", SHA256: floorSHA})
		},
		"secret":       func(c *floorCase) { c.tree.Files[0].Secret = true },
		"not on M0":    func(c *floorCase) { c.tree.OnM0 = false },
		"executable":   func(c *floorCase) { c.tree.Files[0].Mode = "100755" },
		"deleted file": func(c *floorCase) { c.tree.Files[0].Kind, c.tree.Files[0].SHA256 = "D", "" },
	}
	for name, edit := range stale {
		t.Run("stale/"+name, func(t *testing.T) {
			c := floorWait(t)
			g := c.grant(t)
			edit(c)
			if _, d := c.gate(t); d.Allowed || d.Reason != "grant_stale" {
				t.Fatalf("gate: %+v", d)
			}
			if c.count(t, `body_json->'landing_grant_refused'->>'grant_id'='`+g.GrantID+`' AND body_json->'landing_grant_refused'->>'reason'='grant_stale'`) != 1 ||
				c.count(t, `body_json->'landing'->>'kind'='main_intent'`) != 0 || c.usable(t) {
				t.Fatal("grant not spent, or pushed")
			}
		})
	}
	held := map[string]struct {
		edit   func(*floorCase)
		reason string
	}{
		"red CI":     {func(c *floorCase) { c.ci.Status = "Failed" }, "ci_red"},
		"CI running": {func(c *floorCase) { c.ci.Status = "Running" }, "ci_unknown"},
		"window":     {func(c *floorCase) { c.window = true }, "window"},
		"moved main": {func(c *floorCase) { c.main = commitC }, "main_changed"},
	}
	for name, tc := range held {
		t.Run("held/"+name, func(t *testing.T) {
			c := floorWait(t)
			c.grant(t)
			tc.edit(c)
			if _, d := c.gate(t); d.Allowed || d.Reason != tc.reason {
				t.Fatalf("gate: %+v", d)
			}
			if c.count(t, `body_json ? 'landing_grant_refused'`) != 0 || c.count(t, `body_json->'landing'->>'kind'='main_intent'`) != 0 {
				t.Fatal("grant spent, or pushed")
			}
			// Held, not discarded: the operation still waits and its grant
			// is still usable.
			if c.count(t, `body_json->'landing'->>'kind'='discarded'`) != 0 || landingState(t, c.w, infraStream).Operations[c.op].Phase != "awaiting_grant" {
				t.Fatal("the waiting operation was discarded")
			}
			if !c.usable(t) {
				t.Fatal("grant not usable")
			}
			if st, err := store.ReadLandingWait(ctx, c.w.rt, c.op); err != nil || st == nil || len(st.Grants) != 1 || !st.Grants[0].Valid {
				t.Fatalf("status: %+v %v", st, err)
			}
			// Once main, CI and the window are ready again, the same grant
			// pushes R.
			c.main, c.ci.Status, c.window = commitA, "Succeeded", false
			if auth, d := c.gate(t); !d.Allowed || auth.R != commitB {
				t.Fatalf("held grant did not push once ready: %+v", d)
			}
		})
	}
}

// Serial: FreezeEventClock and the signing anchor. P1 of attempt 1's
// review: a grant still valid when the gate starts, which expires while
// M0..R is read from Git, authorizes nothing: the gate decides at the clock
// after its Git reads.
func TestFloorGrantExpiresDuringGitReads(t *testing.T) {
	c := floorWait(t)
	start := c.at.Add(3 * time.Minute) // floorWait's clock
	a := c.approval(store.ActionFloorLanding)
	a.ApprovedAt = start.Add(30*time.Second - store.LandingGrantTTL) // expires 30 s after start
	g, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, a, c.params(t))
	ok(t, err)
	if !c.usable(t) {
		t.Fatal("grant not usable before its expiry")
	}
	read := c.src.FloorFiles
	c.src.FloorFiles = func(ctx context.Context, repo, m0, r string) (landing.FloorTree, error) {
		store.FreezeEventClock(start.Add(31 * time.Second)) // a slow read, past the expiry
		return read(ctx, repo, m0, r)
	}
	if start.UTC().Format("2006-01-02") != start.Add(31*time.Second).UTC().Format("2006-01-02") {
		t.Skip("the read would cross midnight")
	}
	if _, d := c.gate(t); d.Allowed || d.Reason != "grant_expired" {
		t.Fatalf("gate: %+v", d)
	}
	if c.count(t, `body_json->'landing'->>'kind'='main_intent'`) != 0 || c.count(t, `body_json ? 'landing_grant_use'`) != 0 ||
		c.count(t, `body_json->'landing_grant_refused'->>'grant_id'='`+g.GrantID+`'`) != 0 {
		t.Fatal("an expired grant pushed, or was recorded refused")
	}
}

// Serial: FreezeEventClock and the signing anchor. (b) and (d): a material
// over a never-grantable tree cannot be granted; a guardrail or core grant
// is never written for infra, and a floor grant never for platform.
func TestFloorGrantKinds(t *testing.T) {
	c := floorWait(t)
	for _, action := range []string{store.ActionGuardrailLanding, store.ActionCoreLanding} {
		_, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(action), c.params(t))
		refusedWith(t, err, "grant_unsupported")
		if store.GrantActionSupported(action, infraRepo) {
			t.Fatal(action, "supported for infra")
		}
	}
	c.tree.Files = append(c.tree.Files, landing.FloorFile{Path: "k8s/CLAUDE.md", Kind: "A", Mode: "100644", SHA256: floorSHA})
	if m := c.material(t); !slices.Contains(m.Problems, "floor") {
		t.Fatalf("a never-grantable file in M0..R is grantable: %v", m.Problems)
	}
	_, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionFloorLanding), c.params(t))
	refusedWith(t, err, "floor")
	c.tree.Files = c.tree.Files[:1]
	// A floor grant written by hand for platform, or a core one for infra,
	// is refused by the stream itself.
	g := c.grant(t)
	core := g
	core.GrantID, core.ApprovalID, core.Action = "lg_appr_floor_core", "appr_floor_core", store.ActionCoreLanding
	core.FloorPaths = nil
	err = appendGrant(t, c, infraStream, core)
	refusedWith(t, err, "landing_grant")
	if !store.GrantActionSupported(store.ActionFloorLanding, infraRepo) || store.GrantActionSupported(store.ActionFloorLanding, "bifrost-platform") {
		t.Fatal("floor grant support")
	}
	// Revocable on its own stream, as the other grants.
	rv, err := store.RevokeLandingGrant(ctx, c.w.rt, g.GrantID)
	ok(t, err)
	if !rv.Revoked || rv.Op != c.op || c.usable(t) || c.count(t, `body_json ? 'landing_grant_revoked'`) != 1 {
		t.Fatalf("revoke: %+v", rv)
	}
	if _, d := c.gate(t); d.Allowed {
		t.Fatal("a revoked grant pushed")
	}

	p := c5Wait(t, c5Core, false, true)
	params := p.params(t)
	_, err = store.WriteLandingGrant(ctx, p.w.rt, p.gsrc, p.approval(store.ActionFloorLanding, time.Minute), params)
	refusedWith(t, err, "grant_unsupported")
	if n := p.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant'`, landingRepoStream); n != 0 {
		t.Fatal("platform floor grant written", n)
	}
}

func appendGrant(t *testing.T, c *floorCase, stream string, g store.LandingGrant) error {
	t.Helper()
	tx, err := c.w.rt.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: "controller", TaskID: g.Task,
		Body: map[string]any{"text": "test: a hand-written grant", "landing_grant": g.Body()}})
	return err
}
