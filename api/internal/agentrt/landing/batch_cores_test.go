package landing

import (
	"slices"
	"testing"
	"time"
)

// coreCand is batchCand n changing a core path with grants enabled: a root
// that may wait for a grant.
func coreCand(t *testing.T, n int, now time.Time) BatchCandidate {
	t.Helper()
	c := batchCand(n)
	f := c.Facts
	f.GrantEnabled = true
	f.Changes = slices.Clone(f.Changes)
	f.Changes[0].OldPath, f.Changes[0].NewPath = "api/internal/agentrt/store/example.go", "api/internal/agentrt/store/example.go"
	if reason, soft := PolicyChecks(f, now); reason != "" || len(soft) == 0 || !MayAwaitGrant(f, soft) {
		t.Fatal("fixture: not a root that may wait for a grant", reason, soft)
	}
	c.Facts = f
	return c
}

// R5:5.7 batch of cores: two and three roots that may wait for a grant
// form one batch, the first of them leading and the others in their order.
func TestBatchCoresFormOneBatch(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	two := []BatchCandidate{batchCand(0), coreCand(t, 1, now), coreCand(t, 2, now)}
	if got := batchTasks(FormBatch(two, now)); !slices.Equal(got, []string{two[1].Task, two[0].Task, two[2].Task}) {
		t.Fatal("two core roots", got)
	}
	three := []BatchCandidate{coreCand(t, 0, now), coreCand(t, 1, now), batchCand(2), coreCand(t, 3, now)}
	if got := batchTasks(FormBatch(three, now)); !slices.Equal(got, batchTasks(three)) {
		t.Fatal("three core roots", got)
	}
	only := []BatchCandidate{coreCand(t, 0, now), coreCand(t, 1, now), coreCand(t, 2, now)}
	if got := batchTasks(FormBatch(only, now)); !slices.Equal(got, batchTasks(only)) {
		t.Fatal("a batch of cores alone", got)
	}
}

// A root its claim refuses on policy (soft checks with grants off, or a
// floor) joins no batch: the controller claims it alone. The cores around
// it still form one batch.
func TestBatchCoresRefusedRootClaimedAlone(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	off := coreCand(t, 1, now)
	off.Facts.GrantEnabled = false
	if d := EligibleToClaim(off.Facts, now); d.Allowed {
		t.Fatal("fixture: a core root with grants off is claimed", d)
	}
	floor := batchCand(3)
	floor.Facts.Changes = slices.Clone(floor.Facts.Changes)
	floor.Facts.Changes[0].NewMode = "120000"
	if reason, _ := PolicyChecks(floor.Facts, now); reason == "" {
		t.Fatal("fixture: not refused on policy")
	}
	cands := []BatchCandidate{coreCand(t, 0, now), off, coreCand(t, 2, now), floor, batchCand(4)}
	if got := batchTasks(FormBatch(cands, now)); !slices.Equal(got, []string{cands[0].Task, cands[2].Task, cands[4].Task}) {
		t.Fatal("a refused root in a batch", got)
	}
	if got := FormBatch([]BatchCandidate{off}, now); len(got) != 0 {
		t.Fatal("a refused root formed a batch", batchTasks(got))
	}
}
