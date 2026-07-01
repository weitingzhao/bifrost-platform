package agentgovernance

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

func TestComputeTrustMatrix_PromotionEligible(t *testing.T) {
	now := time.Now().UTC()
	jobs := []remediation.Job{
		{Scope: "nightly-health-check", Status: remediation.JobDone, CreatedAt: now, UpdatedAt: now},
		{Scope: "nightly-health-check", Status: remediation.JobDone, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{Scope: "nightly-health-check", Status: remediation.JobDone, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
	}
	resp := ComputeTrustMatrix(jobs)
	var health *TrustMatrixEntry
	for i := range resp.Entries {
		if resp.Entries[i].SkillID == "nightly-health" {
			health = &resp.Entries[i]
			break
		}
	}
	if health == nil {
		t.Fatal("nightly-health entry missing")
	}
	if health.ConsecutiveSuccesses != 3 {
		t.Fatalf("successes %d", health.ConsecutiveSuccesses)
	}
	// L0 default — promotion eligible applies to L1 skills; health stays L0
	if health.PromotionEligible {
		t.Fatal("L0 skill should not promote")
	}
}

func TestComputeTrustMatrix_DemotionOnFailure(t *testing.T) {
	now := time.Now().UTC()
	jobs := []remediation.Job{
		{Scope: "release", Status: remediation.JobFailed, CreatedAt: now, UpdatedAt: now},
	}
	resp := ComputeTrustMatrix(jobs)
	var release *TrustMatrixEntry
	for i := range resp.Entries {
		if resp.Entries[i].SkillID == "release" {
			release = &resp.Entries[i]
			break
		}
	}
	if release == nil || !release.DemotionTriggered {
		t.Fatal("expected demotion on latest failure")
	}
}

func TestComputeCapabilityMap_NoMissingCoreTools(t *testing.T) {
	resp := ComputeCapabilityMap()
	if resp.McpToolCount < 40 {
		t.Fatalf("mcp tools %d", resp.McpToolCount)
	}
	for _, e := range resp.Entries {
		if e.TaskScope == "post-fix-verification" && e.HasGap {
			t.Fatalf("post-fix gap: %s", e.GapDetail)
		}
	}
}

func TestComputePerformance_Windows(t *testing.T) {
	now := time.Now().UTC()
	jobs := []remediation.Job{
		{Status: remediation.JobDone, CreatedAt: now, UpdatedAt: now.Add(30 * time.Second)},
		{Status: remediation.JobFailed, CreatedAt: now, UpdatedAt: now.Add(20 * time.Second)},
	}
	resp := ComputePerformance(jobs)
	if len(resp.Windows) != 2 {
		t.Fatalf("windows %d", len(resp.Windows))
	}
	if resp.Windows[0].TotalExecutions != 2 {
		t.Fatalf("total %d", resp.Windows[0].TotalExecutions)
	}
}

func TestApplyTrustOverrides_Level(t *testing.T) {
	raw := computeTrustMatrixRaw(nil)
	overrides := map[string]TrustOverride{
		"release": {
			SkillID: "release", Level: "L0", AppliedBy: "owner",
			AppliedAt: time.Now().UTC(),
		},
	}
	merged := ApplyTrustOverrides(raw, overrides)
	var release *TrustMatrixEntry
	for i := range merged.Entries {
		if merged.Entries[i].SkillID == "release" {
			release = &merged.Entries[i]
			break
		}
	}
	if release == nil || release.CurrentLevel != "L0" {
		t.Fatalf("override level %v", release)
	}
	if release.LastOverrideBy != "owner" {
		t.Fatalf("override by %q", release.LastOverrideBy)
	}
}
