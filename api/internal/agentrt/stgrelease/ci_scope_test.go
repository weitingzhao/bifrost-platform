package stgrelease

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A stale incremental green leaves P Missing; the observation keeps its
// incremental scope and the last full green, and a ci_missing block records
// them in detail.ci_scope for the card, outside the interval's identity.
func TestCIBlockRecordsWhyPWaits(t *testing.T) {
	t.Parallel()
	c, l, _, _ := controlFixture(t, "Succeeded")
	l.entries = l.entries[:3]
	s := l.state(t)
	b := s.Batches[leaseBatch]
	base, _ := store.StgTrustedBaseline(s)
	now := l.clock.Now()
	e := landingci.ReleaseEvidence{ReadAt: now, Stale: []string{"inc"}, LastFullRun: "full-ci", LastFullCommit: sha("c"), LastFullAt: now.Add(-51 * time.Hour)}
	e.Status = "Missing"
	obs := ReleaseCIObservation(e)
	if obs.Scope != landingci.APIScopeIncremental || obs.Stale != 1 || obs.LastFullRun != "full-ci" || obs.FullMaxAge != landingci.FullMaxAge || obs.Accepted != nil {
		t.Fatalf("observation dropped the CI scope: %+v", obs)
	}
	if err := c.blockAndFinish(context.Background(), s, leaseBatch, b, "ci_missing", base, &obs); err != nil {
		t.Fatal(err)
	}
	var block store.StgBlock
	for _, v := range l.state(t).Blocks {
		block = v
	}
	scope := store.StgRecord(block.Opened.Record["detail"].(map[string]any)["ci_scope"].(map[string]any))
	if scope.String("scope") != "incremental" || scope.Int("stale") != 1 || scope.String("last_full_run") != "full-ci" || scope.String("last_full_commit") != sha("c") || scope.Int("last_full_age_h") != 51 || scope.Int("cap_h") != 48 {
		t.Fatalf("block detail.ci_scope = %v", scope)
	}
}
