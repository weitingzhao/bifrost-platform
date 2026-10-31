package store_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Real migration/runtime-role tests. The harness skips without PostgreSQL.
// CI and immutable Git trees are doubles; all claims, keys, locks, quota and
// intents below use the production store/controller functions.
// Serial: FreezeEventClock.
func TestS5PGFirstClaimRecoveryAndFinalGate(t *testing.T) {
	for _, variant := range []string{"pass", "missing", "forged", "old generation", "profile update", "main drift", "latest review fail", "expired", "final review fail", "final expired", "final main drift", "final CI failed", "final profile changed", "checker finding", "freeze retry", "freeze exhausted"} {
		t.Run(variant, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			w, task, src := goalDrill(t, "M-3", now.Add(-time.Minute))
			fence := landingState(t, w, landingRepoStream).Fence()
			restore := store.FreezeEventClock(fence.Add(time.Second))
			_, err := store.EnsureLandingMode(ctx, w.rt, "bifrost-platform")
			restore()
			ok(t, err)
			defer store.FreezeEventClock(now)()
			body := strings.Replace(landingRuleBody, `allow: ["console/src/components/**/*.tsx"]`, `allow: ["console/src/components/**/*.tsx", "`+strings.Join(rules.S5Paths(), `", "`)+`"]`, 1)
			// The deployment's trust root is the test's policy below: the S5
			// rule may be signed (design §9 order, store.S5SigningReady).
			saved := store.S5SigningReady
			store.S5SigningReady = func(context.Context) error { return nil }
			_, err = signer{t, rptest.NewKey(t)}.sign(w, ruleText("landing:dev", 2, "loosen", since, until, body))
			store.S5SigningReady = saved
			ok(t, err)
			path := rules.S5Paths()[0]
			sum := strings.Repeat("a", 64)
			r := strings.Repeat("d", 40)
			policy := landing.S5Policy{ProfileDigest: sum, CheckerImageDigest: "sha256:" + sum, ToolchainDigest: sum, CallersDigest: sum, TypesDigest: sum, TestsDigest: sum, UISHA: strings.Repeat("c", 40)}
			src.S5Policy = func(context.Context) (landing.S5Policy, error) { return policy, nil }
			src.Result = func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
				return true, []rules.LandingChange{{Kind: "M", OldPath: path, NewPath: path, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "return value"}}, nil
			}
			src.S5Patch = func(_ context.Context, repo, task, base, a, m0, r string) (landing.PatchEvidence, error) {
				p := landing.PatchEvidence{A: a, Base: base, ParentA: base, M0: m0, R: r, SourcePatchSHA256: sum, SourceStablePatchID: commitA, DiffAlgorithmVersion: "git-stable-v1", OldNewPathsAndModes: []landing.S5PathMode{{OldPath: path, NewPath: path, OldMode: "100644", NewMode: "100644", Kind: "M"}}}
				if r != "" {
					p.ParentR = m0
					p.ReplayMatch = true
					p.ReplayPatchSHA256 = sum
					p.ReplayStablePatchID = commitA
					p.BaselineTree = m0
					p.ResultTree = r
				}
				return p, nil
			}
			op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", task, now)
			ok(t, err)
			if !d.Allowed || op == "" {
				t.Fatal("first claim", d)
			}
			state := landingState(t, w, landingRepoStream)
			if !state.Operations[op].S5.Required || state.Operations[op].S5.Key.Generation != 0 {
				t.Fatal("claim requirement missing")
			}
			assertNoMain := func() {
				t.Helper()
				q := landingState(t, w, "landing-quota:dev:"+now.Format("2006-01-02"))
				o := landingState(t, w, landingRepoStream).Operations[op]
				if len(q.Reservations) != 0 || o.AtMain() || o.EverLanded {
					t.Fatal("charged before proof", q, o)
				}
			}
			assertNoMain()
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "prepared", "R", r, "M0", commitA, "committer_date", now.Format(time.RFC3339Nano), "dry_run", false)))
			o, d, err := store.PrepareS5Evidence(ctx, w.rt, src, "prod", op, nil)
			ok(t, err)
			if !d.Allowed || o.S5.Key.Generation != 1 {
				t.Fatal(d, o)
			}
			// Process restart and duplicate reconciler: exact key survives, no bump.
			again, d, err := store.PrepareS5Evidence(ctx, w.rt, src, "prod", op, nil)
			ok(t, err)
			if !d.Allowed || again.S5.Key != o.S5.Key {
				t.Fatal("restart", d, again)
			}
			ref := "refs/heads/land/prod/" + task + "/1"
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "branch_intent", "ref", ref, "R", r)))
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "branch_pushed", "ref", ref, "R", r)))
			assertNoMain()
			store.FreezeEventClock(now.Add(5 * time.Minute))
			finalGate := false
			src.S5CI = func(_ context.Context, k landing.S5CIKey, uid string) (landing.S5CIResult, error) {
				name, _ := k.RunName()
				kd, _ := k.Digest()
				// The checker's evidence names exactly the change's S5 file.
				files := []landing.S5FileEvidence{{Path: path, BeforeBlob: commitA, AfterBlob: r}}
				ci := landing.S5CIResult{Key: k, Run: name, UID: uid, KeyDigest: kd, Status: "Succeeded", CreatedAt: now.Add(time.Minute), CompletedAt: now.Add(2 * time.Minute), IdentityVerified: true, Evidence: &landing.S5ContentEvidence{S5ContentBinding: landing.S5BindingForKey(k), CIUID: uid, KeyDigest: kd, Files: files, Passed: true}}
				if finalGate && variant == "final CI failed" {
					ci.Status = "Failed"
				}
				// The release freeze check refused generation 1 (or every
				// generation): an infrastructure failure, not evidence.
				if variant == "freeze exhausted" || variant == "freeze retry" && k.Generation == 1 {
					ci.Status, ci.Reason, ci.Retryable, ci.IdentityVerified, ci.Evidence = "Failed", "S5_EVIDENCE_MISSING", true, false, nil
				}
				switch variant {
				case "checker finding":
					ci.Status, ci.Reason, ci.IdentityVerified, ci.Evidence = "Failed", "S5_CALLER_CHANGED", false, nil
				case "missing":
					ci.Evidence = nil
				case "forged":
					ci.IdentityVerified = false
				case "old generation":
					ci.Key.Generation++
				}
				return ci, nil
			}
			failReview := func() {
				_, err = w.db.Connect(t, w.db.Admin).Exec(ctx, `INSERT INTO agentrt.verification(verification_id,task_id,attempt_id,method,verdict,evidence_artifact_id,verifier_attempt_id,subject_commit,created_at)
 SELECT $2,task_id,attempt_id,'review','fail',evidence_artifact_id,verifier_attempt_id,subject_commit,clock_timestamp()+interval '1 second' FROM agentrt.verification WHERE task_id=$1 AND method='review' ORDER BY created_at DESC LIMIT 1`, task, store.NewVerificationID())
				ok(t, err)
			}
			pusher := &livePusher{outcome: landinggit.PushOK}
			ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, OpenLive: func(context.Context, string, string) (controller.LiveWorkspace, error) {
				finalGate = true
				if variant == "final review fail" {
					failReview()
				}
				if variant == "final expired" {
					store.FreezeEventClock(at(until))
				}
				if variant == "final profile changed" {
					policy.TestsDigest = strings.Repeat("e", 64)
				}
				return pusher, nil
			}}
			sources := controller.DrillSources{Facts: src, Main: func(context.Context, string) (string, error) {
				if variant == "main drift" || finalGate && variant == "final main drift" {
					return strings.Repeat("e", 40), nil
				}
				return commitA, nil
			}, WindowBlocked: func(context.Context, string) (bool, error) { return false, nil }, CI: func(context.Context, string, string, time.Time) (landing.CIRun, error) {
				t.Fatal("S5 fell back to legacy CI")
				return landing.CIRun{}, nil
			}, EnsureS5CI: func(_ context.Context, key landing.S5CIKey) (string, error) {
				persisted := landingState(t, w, landingRepoStream).Operations[op].S5.Key
				if persisted != key {
					t.Fatal("Ensure before key commit")
				}
				return fmt.Sprintf("uid-%d", key.Generation), nil
			}}
			if variant == "profile update" {
				policy.ProfileDigest = strings.Repeat("f", 64)
			}
			if variant == "latest review fail" {
				failReview()
			}
			if variant == "expired" {
				store.FreezeEventClock(at(until))
			}
			_, err = ctl.LandingDrillCI(ctx, sources, op, store.DecisionAt())
			ok(t, err)
			if strings.HasPrefix(variant, "freeze") {
				// Inside each backoff the operation waits; after it, one new
				// generation with the same material, at most S5InfraRetries.
				for n := 0; n < landing.S5InfraRetries; n++ {
					before := landingState(t, w, landingRepoStream).Operations[op]
					if before.Terminal() || before.S5.InfraRetries != n {
						t.Fatalf("retry %d: %+v", n, before.S5)
					}
					store.FreezeEventClock(before.S5.KeyAt.Add(landing.S5InfraRetryAfter(n)))
					_, err = ctl.LandingDrillCI(ctx, sources, op, store.DecisionAt())
					ok(t, err)
					after := landingState(t, w, landingRepoStream).Operations[op]
					same := after.S5.Key
					same.Generation = before.S5.Key.Generation
					if after.S5.Key.Generation != before.S5.Key.Generation+1 || same != before.S5.Key || after.S5.InfraRetries != n+1 {
						t.Fatalf("retry %d did not open the next generation: %+v", n, after.S5)
					}
					_, err = ctl.LandingDrillCI(ctx, sources, op, store.DecisionAt())
					ok(t, err)
					if variant == "freeze retry" {
						break
					}
				}
				if variant == "freeze exhausted" {
					o := landingState(t, w, landingRepoStream).Operations[op]
					store.FreezeEventClock(o.S5.KeyAt.Add(landing.S5InfraRetryAfter(landing.S5InfraRetries)))
					_, err = ctl.LandingDrillCI(ctx, sources, op, store.DecisionAt())
					ok(t, err)
				}
			}
			// B2: a deterministic S5 refusal or a terminal result for the
			// generation ends the operation with its code and frees the
			// repository's in-flight slot; only waiting cases stay live.
			want := map[string]string{"missing": "S5_EVIDENCE_MISSING", "forged": "S5_CI_IDENTITY", "old generation": "S5_GENERATION_STALE",
				"latest review fail": "S5_REVIEW_BINDING", "checker finding": "S5_CALLER_CHANGED", "freeze exhausted": "S5_EVIDENCE_MISSING"}[variant]
			final := landingState(t, w, landingRepoStream)
			if o := final.Operations[op]; want != "" {
				active := 0
				for _, other := range final.Operations {
					if other.Active() {
						active++
					}
				}
				if o.Phase != "failed" || o.Outcome != want || o.Active() || active != 0 || !o.BlocksClaim() {
					t.Fatalf("S5 refusal kept the slot: %+v (active %d)", o, active)
				}
			} else if landing.S5Code(o.Outcome) {
				// Policy waits keep their own ending (here the root's bound).
				t.Fatal("ended with an S5 code", variant, o.Outcome)
			}
			if variant == "pass" || variant == "profile update" || variant == "freeze retry" {
				if len(pusher.pushes) != 1 {
					t.Fatal("no single authorized push", pusher.pushes)
				}
				if variant == "profile update" && landingState(t, w, landingRepoStream).Operations[op].S5.Key.Generation != 2 {
					t.Fatal("profile reused generation")
				}
			} else {
				assertNoMain()
				if len(pusher.pushes) != 0 {
					t.Fatal("pushed without proof")
				}
			}
		})
	}
}
