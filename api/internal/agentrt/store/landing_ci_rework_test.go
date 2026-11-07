package store_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7 goal 1 (store/precheck.go): a red landing CI sends the attempt back
// to its executor with the failing output, whatever accepted it and in its
// last round too, and the Owner gets no card for it. Precheck verify keeps
// the card.

// checkedRed is a root whose attempt the deterministic check accepted (as
// when the landing ran no CI at its submission), with its landing operation
// prepared and its branch pushed, and max rounds as given (0: the default).
func checkedRed(t *testing.T, maxRounds int) (*world, string, string, time.Time) {
	t.Helper()
	w, root, at := ciRoot(t)
	if maxRounds > 0 {
		w.legacy(t, fmt.Sprintf(`UPDATE agentrt.task SET max_rounds = %d WHERE task_id = '%s'`, maxRounds, root))
	}
	if c := w.run(t, "h1", cursor, "done"); c.TaskID != root {
		t.Fatalf("claimed %s, not %s", c.TaskID, root)
	}
	v, accepted, err := store.StartVerificationWith(ctx, w.rt, root, store.ConcludeOptions{Precheck: store.PrecheckVerify})
	ok(t, err)
	if v == "" || accepted {
		t.Fatalf("submission: check %q, accepted %v", v, accepted)
	}
	w.run(t, "h2", deterministic, "done")
	if did, err := store.ConcludeWith(ctx, w.rt, v, store.ConcludeOptions{}); err != nil || did != "accepted" {
		t.Fatalf("the check did %q (%v)", did, err)
	}
	if got := verificationsOf(t, w, root); !slices.Equal(got, []string{"deterministic/pass/accepts"}) {
		t.Fatalf("verifications: %v", got)
	}
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, c4Source(precheckPath), "prod", root, at)
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatal("claim", d)
	}
	c4Prepare(t, w, root, op, at.Add(-29*time.Minute), true)
	store.FreezeEventClock(at.Add(-20 * time.Minute))
	return w, root, op, at
}

// rounds are root's round_no and max_rounds.
func rounds(t *testing.T, w *world, root string) (int, int) {
	t.Helper()
	var round, maxRounds int
	ok(t, w.rt.QueryRow(ctx, `SELECT round_no, max_rounds FROM agentrt.task WHERE task_id = $1`, root).Scan(&round, &maxRounds))
	return round, maxRounds
}

// reworkReason is the reason of root's move to rework.
func reworkReason(t *testing.T, w *world, root string) string {
	t.Helper()
	var why string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->>'reason', '') FROM agentrt.event WHERE task_id = $1
	  AND event_type = 'task_state_changed' AND body_json->>'to' = 'rework' ORDER BY event_id DESC LIMIT 1`, root).Scan(&why))
	return why
}

// Serial: FreezeEventClock. An attempt the deterministic check accepted
// whose landing CI is red goes back to ready in round 2: the failed
// landing_ci verification names the operation, and its evidence the CI's
// failure. No root_failed card.
func TestLandingCIReworkCheckedAttemptGoesBack(t *testing.T) {
	w, root, op, at := checkedRed(t, 0)
	reworked, err := store.RecordLandingCIRed(ctx, w.rt, "bifrost-platform", op, "test: failed", redCI, store.PrecheckCI)
	ok(t, err)
	if !reworked || w.state(t, "task", root) != "ready" {
		t.Fatalf("reworked %v, state %s", reworked, w.state(t, "task", root))
	}
	if p, why := phaseOf(t, w, op); p != "failed" || why != "ci_red" {
		t.Fatal("operation", p, why)
	}
	if round, _ := rounds(t, w, root); round != 2 {
		t.Fatal("round", round)
	}
	if v := verificationsOf(t, w, root); !slices.Equal(v, []string{"deterministic/pass/accepts", "landing_ci/fail"}) {
		t.Fatalf("verifications: %v", v)
	}
	var ref, evidence string
	ok(t, w.rt.QueryRow(ctx, `SELECT v.check_ref, convert_from(b.body, 'UTF8') FROM agentrt.verification v
	  JOIN agentrt.artifact_blob b ON b.artifact_id = v.evidence_artifact_id
	  WHERE v.task_id = $1 AND v.method = $2 AND v.verdict = 'fail'`, root, store.MethodLandingCI).Scan(&ref, &evidence))
	if ref != store.MethodLandingCI+":"+op {
		t.Fatal("check_ref", ref)
	}
	for _, want := range []string{op, redCI.Run, redCI.TaskRuns[0].Name, "FAIL: TestPrecheck"} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("the evidence does not name %q: %s", want, evidence)
		}
	}
	if why := reworkReason(t, w, root); !strings.Contains(why, "the landing CI of "+op+" failed in round 1") {
		t.Fatal("why", why)
	}
	if choices := rootFailed(t, w, root, at); choices != nil {
		t.Fatal("a root_failed card for a rework:", choices)
	}
}

// Serial: FreezeEventClock. A replay that does not merge onto main ends
// its operation failed{conflict} and sends the root back as a red CI does:
// the failed landing_ci check's evidence names M0 and the conflicting
// paths. No root_failed card.
func TestLandingConflictReworkNamesThePaths(t *testing.T) {
	w, root, op, at := checkedRed(t, 0)
	m0 := strings.Repeat("e", 40)
	reworked, err := store.RecordLandingConflict(ctx, w.rt, "bifrost-platform", op, "test: failed", m0, []string{"api/a.go", "api/b.go"}, store.PrecheckCI)
	ok(t, err)
	if !reworked || w.state(t, "task", root) != "ready" {
		t.Fatalf("reworked %v, state %s", reworked, w.state(t, "task", root))
	}
	if p, why := phaseOf(t, w, op); p != "failed" || why != "conflict" {
		t.Fatal("operation", p, why)
	}
	var evidence string
	ok(t, w.rt.QueryRow(ctx, `SELECT convert_from(b.body, 'UTF8') FROM agentrt.verification v
	  JOIN agentrt.artifact_blob b ON b.artifact_id = v.evidence_artifact_id
	  WHERE v.task_id = $1 AND v.method = $2 AND v.verdict = 'fail'`, root, store.MethodLandingCI).Scan(&evidence))
	for _, want := range []string{op, m0, "conflict_paths", "api/a.go", "api/b.go"} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("the evidence does not name %q: %s", want, evidence)
		}
	}
	if why := reworkReason(t, w, root); !strings.Contains(why, "the replay of "+op+" onto main conflicted in round 1") {
		t.Fatal("why", why)
	}
	if choices := rootFailed(t, w, root, at); choices != nil {
		t.Fatal("a root_failed card for a rework:", choices)
	}
}

// Serial: FreezeEventClock. A task in its last round goes back too, its
// max_rounds raised by one, and the move says so. No root_failed card.
func TestLandingCIReworkLastRoundRaisesTheBound(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, int) (*world, string, string, time.Time)
	}{{"accepted on submission", ciRed}, {"accepted by the check", checkedRed}} {
		t.Run(tc.name, func(t *testing.T) {
			w, root, op, at := tc.setup(t, 1)
			reworked, err := store.RecordLandingCIRed(ctx, w.rt, "bifrost-platform", op, "test: failed", redCI, store.PrecheckCI)
			ok(t, err)
			if !reworked || w.state(t, "task", root) != "ready" {
				t.Fatalf("reworked %v, state %s", reworked, w.state(t, "task", root))
			}
			if round, maxRounds := rounds(t, w, root); round != 2 || maxRounds != 2 {
				t.Fatalf("round %d of %d", round, maxRounds)
			}
			if why := reworkReason(t, w, root); why != "the landing CI of "+op+" failed; one more round past its bound" {
				t.Fatal("why", why)
			}
			if choices := rootFailed(t, w, root, at); choices != nil {
				t.Fatal("a root_failed card for a rework:", choices)
			}
		})
	}
}

// Serial: FreezeEventClock. Precheck verify keeps the Owner's card, for an
// attempt the check accepted in its last round as for any.
func TestLandingCIReworkPrecheckVerifyKeepsTheCard(t *testing.T) {
	w, root, op, at := checkedRed(t, 1)
	reworked, err := store.RecordLandingCIRed(ctx, w.rt, "bifrost-platform", op, "test: failed", redCI, store.PrecheckVerify)
	ok(t, err)
	if reworked || w.state(t, "task", root) != "accepted" {
		t.Fatalf("reworked %v, state %s", reworked, w.state(t, "task", root))
	}
	if round, maxRounds := rounds(t, w, root); round != 1 || maxRounds != 1 {
		t.Fatalf("round %d of %d", round, maxRounds)
	}
	if choices := rootFailed(t, w, root, at); !slices.Equal(choices, []string{"try_again", "do_not_merge"}) {
		t.Fatal("card", choices)
	}
}
