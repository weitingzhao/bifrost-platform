package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: acceptedDrill freezes the process event clock. The world fixture
// uses AGENTRT_TEST_PG and skips without it. These are the existing v1 store
// entrypoints, not C-4/C-5's as-yet-unimplemented grant lifecycle.
func TestLandingV1StructuralAndFloorStoreRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*rules.LandingChange)
	}{
		{"core", func(c *rules.LandingChange) { c.NewPath = "api/internal/agentrt/store/new.go" }},
		{"absolute deletion", func(c *rules.LandingChange) { c.OldPath = ".env.local"; c.NewPath = ""; c.NewMode = "" }},
		{"symbolic link", func(c *rules.LandingChange) { c.NewMode = "120000" }},
		{"submodule", func(c *rules.LandingChange) { c.OldMode = "160000" }},
		{"binary", func(c *rules.LandingChange) { c.Binary = true }},
		{"mode only", func(c *rules.LandingChange) { c.NewMode = "100755"; c.Added = 0; c.Deleted = 0 }},
		{"empty endpoints", func(c *rules.LandingChange) { c.OldPath = ""; c.NewPath = ""; c.OldMode = ""; c.NewMode = "" }},
		{"traversal", func(c *rules.LandingChange) { c.NewPath = "../x" }},
	} {
		// Serial: each subtest uses the same event-clock hook via its world.
		t.Run(tc.name, func(t *testing.T) {
			for _, reject := range []bool{false, true} {
				w, task, src := acceptedDrill(t)
				original := src.Result
				src.Result = func(ctx context.Context, repo, root, attempt, a, base string) (bool, []rules.LandingChange, error) {
					valid, changes, err := original(ctx, repo, root, attempt, a, base)
					if reject {
						tc.mutate(&changes[0])
					}
					return valid, changes, err
				}
				op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
				ok(t, err)
				if !reject {
					if !d.Allowed || op == "" {
						t.Fatal("same-entry control", d)
					}
					continue
				}
				if d.Allowed || d.Reason != "floor" || op != "" {
					t.Fatal(d, op)
				}
				state := landingState(t, w, landingRepoStream)
				failed := state.Operations[state.LatestOperation(task)]
				if failed.Phase != "failed" || failed.Outcome != "floor" {
					t.Fatal(failed)
				}
				if len(state.Reservations) != 0 {
					t.Fatal("refusal reserved quota")
				}
			}
		})
	}
}

// Serial: liveReady/FreezeEventClock. Verify the existing final gate rejects
// a changed trusted diff without writing main_intent or reserving quota.
// C-4 now prepares core paths for a grant; absolute/structure still refuse.
func TestLandingV1FinalGateRechecksAbsoluteAndStructure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*rules.LandingChange)
	}{
		{"absolute", func(c *rules.LandingChange) { c.NewPath = "deep/.env.local" }},
		{"core", func(c *rules.LandingChange) { c.NewPath = "api/internal/agentrt/store/zz_new.go" }},
		{"old hard endpoint", func(c *rules.LandingChange) {
			c.OldPath = "api/internal/statefile/x.go"
			c.NewPath = ""
			c.NewMode = ""
		}},
		{"structure", func(c *rules.LandingChange) { c.Binary = true }},
	} {
		// Serial: the fixtures share the global event-clock override.
		t.Run(tc.name, func(t *testing.T) {
			for _, reject := range []bool{false, true} {
				w, _, op, src, prepared := liveReady(t)
				at := prepared.Add(10 * time.Minute)
				restore := store.FreezeEventClock(at)
				original := src.Result
				src.Result = func(ctx context.Context, repo, task, attempt, a, base string) (bool, []rules.LandingChange, error) {
					valid, changes, err := original(ctx, repo, task, attempt, a, base)
					if reject {
						tc.mutate(&changes[0])
					}
					return valid, changes, err
				}
				_, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
				restore()
				ok(t, err)
				phase := landingState(t, w, landingRepoStream).Operations[op].Phase
				quota := landingState(t, w, "landing-quota:dev:"+at.UTC().Format("2006-01-02"))
				if !reject {
					if !d.Allowed || phase != "main_intent" || len(quota.Reservations) != 1 {
						t.Fatal("control", d, phase, quota)
					}
				} else {
					// Grants are not enabled in this world: a core change is
					// floor at the final gate too (review 694 R-03); with
					// them it waits (TestC4*, TestC5*).
					wantReason, wantPhase := "floor", "branch_pushed"
					if d.Allowed || d.Reason != wantReason || phase != wantPhase || len(quota.Reservations) != 0 {
						t.Fatal(d, phase, quota)
					}
				}
			}
		})
	}
}
