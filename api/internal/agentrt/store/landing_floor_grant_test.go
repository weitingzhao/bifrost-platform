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
// is never written for infra, and a platform core change never takes a
// floor grant.
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
	if !store.GrantActionSupported(store.ActionFloorLanding, infraRepo) || !store.GrantActionSupported(store.ActionFloorLanding, "bifrost-platform") {
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
	p.src.FloorFiles = func(context.Context, string, string, string) (landing.FloorTree, error) {
		return landing.FloorTree{OnM0: true, Files: []landing.FloorFile{{Path: c5Core, Kind: "M", Mode: "100644", SHA256: floorSHA}}}, nil
	}
	p.gsrc.Facts.FloorFiles = p.src.FloorFiles
	_, err = store.WriteLandingGrant(ctx, p.w.rt, p.gsrc, p.approval(store.ActionFloorLanding, time.Minute), params)
	refusedWith(t, err, "action_mismatch")
	if n := p.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant'`, landingRepoStream); n != 0 {
		t.Fatal("platform floor grant written for a core change", n)
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

// floor_grant takes the floor grant on both repositories; core takes the
// core grant, a floor_grant beside it included; anything else the guardrail.
func TestGrantActionForRepo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		repo   string
		soft   []string
		action string
	}{
		{infraRepo, []string{"floor_grant", "rule_paths"}, store.ActionFloorLanding},
		{infraRepo, []string{"floor_grant"}, store.ActionFloorLanding},
		{"bifrost-platform", []string{"floor_grant", "rule_paths"}, store.ActionFloorLanding},
		{"bifrost-platform", []string{"core", "floor_grant"}, store.ActionCoreLanding},
		{"bifrost-platform", []string{"core"}, store.ActionCoreLanding},
		{"bifrost-platform", []string{"rule_paths"}, store.ActionGuardrailLanding},
		{"bifrost-platform", []string{"floor_soft"}, store.ActionGuardrailLanding},
	} {
		if got := store.GrantActionFor(tc.repo, tc.soft); got != tc.action {
			t.Errorf("%s %v: %s, want %s", tc.repo, tc.soft, got, tc.action)
		}
	}
	for _, repo := range []string{infraRepo, "bifrost-platform"} {
		if !store.GrantActionSupported(store.ActionFloorLanding, repo) {
			t.Errorf("floor grant unsupported for %s", repo)
		}
	}
}

// The platform fixtures' paths fail what the grant tests rely on: c5Soft
// rule_paths alone (the guardrail grant), c5Floor floor_grant with
// rule_paths (the floor grant), c5Core core.
func TestPlatformGrantFixturePaths(t *testing.T) {
	t.Parallel()
	r, err := rules.Parse([]byte(ruleText("landing:dev", 1, "initial", since, until, c5RuleBody)))
	ok(t, err)
	for path, want := range map[string][]rules.LandingRefusal{
		c5Soft:  {rules.LandingRulePaths},
		c5Floor: {rules.LandingFloorGrant, rules.LandingRulePaths},
		c5Core:  {rules.LandingCore, rules.LandingRulePaths},
	} {
		changes := []rules.LandingChange{{OldPath: path, NewPath: path, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1}}
		soft := rules.LandingSoftRefusals(r.Landing, "bifrost-platform", changes)
		if !slices.Equal(soft, want) || rules.LandingFloorGrantable(r.Landing, "bifrost-platform", changes, soft) != (path == c5Floor) {
			t.Errorf("%s: soft %v, want %v", path, soft, want)
		}
	}
}

// platformFloorGrant is a well-formed platform floor grant of c5Floor, as
// the executor writes it: its pinned files and the platform's pinned run.
func platformFloorGrant() store.LandingGrant {
	now := time.Now().UTC().Truncate(time.Second)
	sha := strings.Repeat("a", 40)
	digest := "sha256:" + strings.Repeat("b", 64)
	task := "t_00000000000000000000000001"
	return store.LandingGrant{V: "1", GrantID: "lg_appr_pf", ApprovalID: "appr_pf", ApprovalNumber: 1,
		Action: store.ActionFloorLanding, ParamsHash: strings.Repeat("c", 64), Home: "prod", Env: "prod", Tenant: "dev", Repo: "bifrost-platform",
		Task: task, Attempt: "a_00000000000000000000000001", Op: "land:" + task + ":1", A: sha, Base: sha, R: sha, M0: sha,
		CommitterDate: now.Format(time.RFC3339Nano), CandidateDigest: digest, MaterialDigest: digest,
		Waives: []string{"floor_grant", "rule_paths"}, SoftAtRequest: []string{"floor_grant", "rule_paths"}, Files: 1, Lines: 2,
		Rule: store.GrantRule{ID: "landing:dev", Version: 1, SHA256: strings.Repeat("d", 64)}, ChecksVersion: 1,
		CI:      store.GrantCI{Pipeline: "bifrost-ci-platform", Run: "green", UID: "uid", CreatedAt: now.Format(time.RFC3339Nano), Status: "Succeeded", SHA: sha},
		Purpose: store.LandingGrantPurpose, ApprovedAt: now.Format(time.RFC3339Nano), GrantedAt: now.Format(time.RFC3339Nano),
		ExpiresAt:  now.Add(store.LandingGrantTTL).Format(time.RFC3339Nano),
		FloorPaths: []store.GrantFloorPath{{Path: c5Floor, Entry: "api/**", BlobSHA256: floorSHA}}}
}

// parseLandingGrant takes a platform floor grant whose floor_paths carry
// rules.FloorEntry's entry in its own repository, and refuses one whose
// entry is another's (or none), whatever the other repository says.
func TestPlatformFloorGrantParse(t *testing.T) {
	t.Parallel()
	if _, err := store.ParseLandingGrantForTest(platformFloorGrant().Body()); err != nil {
		t.Fatal("a well-formed platform floor grant:", err)
	}
	for name, edit := range map[string]func(*store.LandingGrant){
		"another glob": func(g *store.LandingGrant) { g.FloorPaths[0].Entry = "scripts/**" },
		"infra's entry": func(g *store.LandingGrant) {
			g.FloorPaths = []store.GrantFloorPath{{Path: floorManifest, Entry: "k8s/**", BlobSHA256: floorSHA}}
		},
		"core path": func(g *store.LandingGrant) {
			g.FloorPaths[0] = store.GrantFloorPath{Path: c5Core, Entry: "api/**", BlobSHA256: floorSHA}
		},
		"open path":      func(g *store.LandingGrant) { g.FloorPaths[0].Path = "api/internal/cluster/x.go" },
		"no floor_paths": func(g *store.LandingGrant) { g.FloorPaths = nil },
		"core waived": func(g *store.LandingGrant) {
			g.Waives, g.SoftAtRequest = []string{"core", "floor_grant"}, []string{"core", "floor_grant"}
		},
		"pinned CI not R's": func(g *store.LandingGrant) { g.CI.SHA = strings.Repeat("e", 40) },
	} {
		g := platformFloorGrant()
		edit(&g)
		if _, err := store.ParseLandingGrantForTest(g.Body()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A core grant on platform waives a floor_grant beside core; it pins no
	// files.
	core := platformFloorGrant()
	core.Action, core.FloorPaths = store.ActionCoreLanding, nil
	core.Waives, core.SoftAtRequest = []string{"core", "floor_grant"}, []string{"core", "floor_grant"}
	if _, err := store.ParseLandingGrantForTest(core.Body()); err != nil {
		t.Fatal("core grant waiving floor_grant:", err)
	}
	// Infra's floor grant is unchanged: its own entries pass, a platform
	// path with a platform entry does not.
	infra := platformFloorGrant()
	infra.Repo, infra.CI.Pipeline = infraRepo, "bifrost-ci-infra"
	infra.FloorPaths = []store.GrantFloorPath{{Path: floorManifest, Entry: "k8s/**", BlobSHA256: floorSHA}}
	if _, err := store.ParseLandingGrantForTest(infra.Body()); err != nil {
		t.Fatal("infra floor grant:", err)
	}
	infra.FloorPaths = []store.GrantFloorPath{{Path: c5Floor, Entry: "api/**", BlobSHA256: floorSHA}}
	if _, err := store.ParseLandingGrantForTest(infra.Body()); err == nil {
		t.Fatal("infra floor grant pinned a platform entry")
	}
}

// The final gate's file check, per repository: the pinned files exactly,
// each with FloorEntry's entry in the operation's repository.
func TestFloorTreeStalePerRepo(t *testing.T) {
	t.Parallel()
	pinned := []store.GrantFloorPath{{Path: c5Floor, Entry: "api/**", BlobSHA256: floorSHA}}
	file := landing.FloorFile{Path: c5Floor, Kind: "M", Mode: "100644", SHA256: floorSHA}
	tree := func(files ...landing.FloorFile) landing.FloorTree { return landing.FloorTree{OnM0: true, Files: files} }
	if why := store.FloorTreeStaleForTest("bifrost-platform", pinned, tree(file)); why != "" {
		t.Fatal("exactly the pinned file:", why)
	}
	changed := file
	changed.SHA256 = strings.Repeat("6", 64)
	extra := landing.FloorFile{Path: "api/internal/research/other.go", Kind: "A", Mode: "100644", SHA256: floorSHA}
	core := landing.FloorFile{Path: c5Core, Kind: "A", Mode: "100644", SHA256: floorSHA}
	for name, tc := range map[string]struct {
		repo string
		tree landing.FloorTree
		want string
	}{
		"changed byte":         {"bifrost-platform", tree(changed), "bytes changed"},
		"extra file":           {"bifrost-platform", tree(file, extra), "extra file"},
		"core file":            {"bifrost-platform", tree(file, core), "extra file"},
		"missing file":         {"bifrost-platform", tree(), "missing file"},
		"not on M0":            {"bifrost-platform", landing.FloorTree{Files: []landing.FloorFile{file}}, "R is not one commit"},
		"another repo's entry": {infraRepo, tree(file), "entry changed"},
	} {
		if why := store.FloorTreeStaleForTest(tc.repo, pinned, tc.tree); !strings.HasPrefix(why, tc.want) {
			t.Errorf("%s: %q, want %q", name, why, tc.want)
		}
	}
	// An entry that is no longer FloorEntry's in the repository spends it.
	if why := store.FloorTreeStaleForTest("bifrost-platform", []store.GrantFloorPath{{Path: c5Floor, Entry: "scripts/**", BlobSHA256: floorSHA}}, tree(file)); !strings.HasPrefix(why, "entry changed") {
		t.Fatal("entry:", why)
	}
	// Infra's check is unchanged.
	infraPinned := []store.GrantFloorPath{{Path: floorManifest, Entry: "k8s/**", BlobSHA256: floorSHA}}
	infraFile := landing.FloorFile{Path: floorManifest, Kind: "M", Mode: "100644", SHA256: floorSHA}
	if why := store.FloorTreeStaleForTest(infraRepo, infraPinned, tree(infraFile)); why != "" {
		t.Fatal("infra:", why)
	}
	gh := landing.FloorFile{Path: ".github/workflows/x.yml", Kind: "A", Mode: "100644", SHA256: floorSHA}
	if why := store.FloorTreeStaleForTest(infraRepo, infraPinned, tree(infraFile, gh)); !strings.HasPrefix(why, "never-grantable") {
		t.Fatal("infra never-grantable:", why)
	}
}

// A floor refusal's detail names its repository's floor paths: platform's
// with grant_supported true, infra's false, each entry FloorEntry's in the
// stream's repository.
func TestFloorRefusalDetailPerRepo(t *testing.T) {
	t.Parallel()
	task := "t_00000000000000000000000001"
	detail := func(supported bool, path, entry string) map[string]any {
		return map[string]any{"soft": []any{"floor_grant", "rule_paths"}, "grant_supported": supported,
			"floor_paths": []any{map[string]any{"path": path, "entry": entry}}}
	}
	event := func(stream string, d map[string]any) store.Event {
		body := map[string]any{"v": "1", "task": task, "reason": "floor", "detail": d}
		return store.Event{Stream: stream, Type: "note", Actor: "controller", TaskID: task, Body: map[string]any{"landing_refusal": body}}
	}
	for name, tc := range map[string]struct {
		stream string
		detail map[string]any
		good   bool
	}{
		"platform":                   {landingRepoStream, detail(true, c5Floor, "api/**"), true},
		"infra":                      {infraStream, detail(false, floorManifest, "k8s/**"), true},
		"platform detail on infra":   {infraStream, detail(true, c5Floor, "api/**"), false},
		"infra detail on platform":   {landingRepoStream, detail(false, floorManifest, "k8s/**"), false},
		"platform with another glob": {landingRepoStream, detail(true, c5Floor, "scripts/**"), false},
		"platform core path":         {landingRepoStream, detail(true, c5Core, "api/**"), false},
	} {
		ev := event(tc.stream, tc.detail)
		bad := store.BadLandingRefusalForTest(ev.Body["landing_refusal"]) != "" || store.RefusalAppendForTest(ev) != nil
		if bad == tc.good {
			t.Errorf("%s: refused %v, want %v", name, bad, !tc.good)
		}
	}
}

const platformFloorFirstLine = "Approving lets agentrt-lander push this commit to platform main, once, fast-forward only, if the files it changes are still exactly these at R and platform CI is green on it."

// platformFloorWait is c5Wait of c5Floor, an unpinned platform floor change
// waiting at the final gate, with its M0..R read from tree.
func platformFloorWait(t *testing.T) (*c5, *landing.FloorTree) {
	t.Helper()
	c := c5Wait(t, c5Floor, false, true)
	tree := &landing.FloorTree{OnM0: true, Files: []landing.FloorFile{{Path: c5Floor, Kind: "M", Mode: "100644", SHA256: floorSHA}}}
	c.src.FloorFiles = func(_ context.Context, repo, m0, r string) (landing.FloorTree, error) {
		if repo != "bifrost-platform" || m0 != commitA || r != commitB {
			t.Fatalf("floor files of %s %s..%s", repo, m0, r)
		}
		return landing.FloorTree{OnM0: tree.OnM0, Files: slices.Clone(tree.Files)}, nil
	}
	c.gsrc.Facts.FloorFiles = c.src.FloorFiles
	if c.phase(t) != "awaiting_grant" {
		t.Fatal("the platform floor change does not wait")
	}
	return c, tree
}

// Serial: FreezeEventClock and the signing anchor. A platform floor change
// takes approve_floor_landing: its material pins the files and the pinned
// CI run, the grant records both, and the final gate pushes R once with
// landing_grant_use.
func TestPlatformFloorGrantPinsFilesAndCI(t *testing.T) {
	c, tree := platformFloorWait(t)
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
	ok(t, err)
	if len(m.Problems) != 0 || m.Action != store.ActionFloorLanding || !slices.Equal(m.SoftAtRequest, []string{"floor_grant", "rule_paths"}) ||
		len(m.FloorPaths) != 1 || m.FloorPaths[0] != (store.GrantFloorPath{Path: c5Floor, Entry: "api/**", BlobSHA256: floorSHA}) ||
		m.Title != "Platform floor change" || m.FirstLine != platformFloorFirstLine || len(m.Candidate.HardCorePaths) != 0 {
		t.Fatalf("material: %+v", m)
	}
	// The pinned CI run and its key are in the candidate, as on the other
	// platform grants.
	if m.Candidate.CI.Run != "green" || m.Candidate.CI.UID != "uid-green" || m.Candidate.CI.Pipeline != "bifrost-ci-platform" || m.Candidate.CI.Siblings["bifrost-ui"] != commitC {
		t.Fatalf("candidate CI: %+v", m.Candidate.CI)
	}
	// The pinned files are in material_digest alone.
	tree.Files[0].SHA256 = strings.Repeat("6", 64)
	other, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
	ok(t, err)
	if other.MaterialDigest == m.MaterialDigest || other.CandidateDigest != m.CandidateDigest {
		t.Fatal("floor_paths are not in material_digest alone")
	}
	tree.Files[0].SHA256 = floorSHA
	// The guardrail grant is not this change's.
	_, err = store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionGuardrailLanding, time.Minute), c.params(t))
	refusedWith(t, err, "action_mismatch")
	g := c.grant(t, store.ActionFloorLanding)
	if g.Action != store.ActionFloorLanding || g.Repo != "bifrost-platform" || !slices.Equal(g.Waives, []string{"floor_grant", "rule_paths"}) ||
		!slices.Equal(g.FloorPaths, m.FloorPaths) || g.MaterialDigest != m.MaterialDigest ||
		g.CI.Pipeline != "bifrost-ci-platform" || g.CI.Run != "green" || g.CI.UID != "uid-green" || g.CI.SHA != commitB {
		t.Fatalf("grant: %+v", g)
	}
	if n := c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant'`, landingRepoStream); n != 1 {
		t.Fatal("grants on landing:bifrost-platform", n)
	}
	auth, d := c.gate(t)
	if !d.Allowed || auth.R != commitB || auth.M0 != commitA {
		t.Fatalf("gate: %+v %+v", d, auth)
	}
	var use string
	ok(t, c.w.rt.QueryRow(ctx, `SELECT body_json->'landing_grant_use'->>'grant_id' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent'`,
		landingRepoStream).Scan(&use))
	if use != g.GrantID || c.phase(t) != "main_intent" {
		t.Fatal("consumption", use, c.phase(t))
	}
	if _, d = c.gate(t); d.Allowed {
		t.Fatal("authorized twice")
	}
}

// Serial: FreezeEventClock and the signing anchor. The final gate of a
// platform floor grant: a changed byte, an extra file or a newer CI run of
// R spend the grant and nothing is pushed; CI not green yet holds it.
func TestPlatformFloorGrantFinalGate(t *testing.T) {
	stale := map[string]func(*c5, *landing.FloorTree){
		"changed byte": func(_ *c5, tree *landing.FloorTree) { tree.Files[0].SHA256 = strings.Repeat("6", 64) },
		"extra file": func(_ *c5, tree *landing.FloorTree) {
			tree.Files = append(tree.Files, landing.FloorFile{Path: "api/internal/research/extra.go", Kind: "A", Mode: "100644", SHA256: floorSHA})
		},
		"core file": func(_ *c5, tree *landing.FloorTree) {
			tree.Files = append(tree.Files, landing.FloorFile{Path: c5Core, Kind: "A", Mode: "100644", SHA256: floorSHA})
		},
		"newer CI run": func(c *c5, _ *landing.FloorTree) {
			c.ci.Run, c.ci.UID, c.ci.CreatedAt = "green-2", "uid-green-2", c.ci.CreatedAt.Add(time.Minute)
		},
	}
	for name, edit := range stale {
		t.Run("stale/"+name, func(t *testing.T) {
			c, tree := platformFloorWait(t)
			g := c.grant(t, store.ActionFloorLanding)
			edit(c, tree)
			if _, d := c.gate(t); d.Allowed || d.Reason != "grant_stale" {
				t.Fatalf("gate: %+v", d)
			}
			if c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing_grant_refused'->>'grant_id'=$2`, landingRepoStream, g.GrantID) != 1 ||
				c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent'`, landingRepoStream) != 0 {
				t.Fatal("grant not spent, or pushed")
			}
			if usable, err := store.HasUsableLandingGrant(ctx, c.w.rt, c.op); err != nil || usable {
				t.Fatal("spent grant usable", err)
			}
		})
	}
	t.Run("held/CI running", func(t *testing.T) {
		c, _ := platformFloorWait(t)
		c.grant(t, store.ActionFloorLanding)
		c.ci.Status = "Running"
		if _, d := c.gate(t); d.Allowed {
			t.Fatalf("gate: %+v", d)
		}
		if c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant_refused'`, landingRepoStream) != 0 {
			t.Fatal("grant spent while CI ran")
		}
		c.ci.Status = "Succeeded"
		if auth, d := c.gate(t); !d.Allowed || auth.R != commitB {
			t.Fatalf("held grant did not push once green: %+v", d)
		}
	})
}
