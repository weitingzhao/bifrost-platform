package controller_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// AGENTRT_LANDING_CI_BASE_PARAM=on alone turns the base param on.
func TestLandingCIBaseSwitch(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "yes": false, "ON": false, "on": true, " on ": true} {
		got := controller.LandingCIBaseParam(func(name string) string {
			if name == "AGENTRT_LANDING_CI_BASE_PARAM" {
				return value
			}
			return ""
		})
		if got != want {
			t.Fatalf("%q: %v", value, got)
		}
	}
}

// A single landing's CI is ensured with its operation's M0: a claimed root
// prepared on M0 and its branch pushed, as landing_live records them.
func TestLandingCIBaseSingleLandingPassesM0(t *testing.T) {
	r := newBatchRig(t, false)
	root := r.accepted(t, "console/src/components/Single.tsx")
	op, claim, err := store.ClaimLandingDrill(ctx, r.pool, r.src.Facts, root, r.now())
	must(t, err)
	if op == "" || !claim.Allowed {
		t.Fatal("claim", op, claim)
	}
	rev := strings.Repeat("5", 40)
	ref := "refs/heads/land/prod/" + root + "/1"
	for _, rec := range []map[string]any{
		{"kind": "prepared", "R": rev, "M0": batchM0, "committer_date": r.now().UTC().Format(time.RFC3339Nano), "dry_run": false},
		{"kind": "branch_intent", "ref": ref, "R": rev},
		{"kind": "branch_pushed", "ref": ref, "R": rev},
	} {
		rec["v"], rec["op"] = 1, op
		must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller",
				Body: map[string]any{"text": "Landing protocol test", "landing": rec}})
			return err
		}))
	}
	r.refs[ref] = rev
	r.clock(31 * time.Second)
	if _, err := r.ctl.LandingDrillCI(ctx, *r.src, op, r.now()); err != nil {
		t.Fatal(err)
	}
	name := landing.CIName(op, 1)
	o := r.state(t).Operations[op]
	if o.M0 != batchM0 || !slices.Equal(r.ensured, []string{name}) || r.bases[name] != batchM0 {
		t.Fatal("CI kicked", o.Phase, r.ensured, r.bases)
	}
}

// A batch's CI (its lead's) is ensured with the batch's own M0.
func TestLandingCIBaseBatchPassesM0(t *testing.T) {
	r := newBatchRig(t, false)
	roots := []string{r.accepted(t, "console/src/components/BaseA.tsx"), r.accepted(t, "console/src/components/BaseB.tsx")}
	b := r.formed(t, roots)
	name := landing.CIName(b.Lead, 1)
	lead := r.state(t).Operations[b.Lead]
	if lead.M0 != batchM0 || lead.R != b.R || r.bases[name] != lead.M0 {
		t.Fatal("CI kicked", r.ensured, r.bases, lead.M0)
	}
}
