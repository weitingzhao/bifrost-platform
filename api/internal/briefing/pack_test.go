package briefing

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestBuildSessionPackDefaultsWithoutContext(t *testing.T) {
	resp := BuildSessionPack(nil, nil, "", "", "", PackRequest{})

	if resp.PackSize != "compact" {
		t.Fatalf("PackSize = %q, want compact", resp.PackSize)
	}
	if resp.Intent != "ops" {
		t.Fatalf("Intent = %q, want ops", resp.Intent)
	}
	if resp.HasBaseline {
		t.Fatal("HasBaseline = true, want false without a baseline")
	}
	if resp.CharCount != len(resp.Pack) {
		t.Fatalf("CharCount = %d, want %d", resp.CharCount, len(resp.Pack))
	}
	if !strings.Contains(resp.Pack, "session_id: —") {
		t.Fatalf("Pack missing dash placeholder for empty session_id:\n%s", resp.Pack)
	}
	if !strings.Contains(resp.Pack, "## Live matrix") {
		t.Fatalf("Pack missing live matrix section:\n%s", resp.Pack)
	}
	if strings.Contains(resp.Pack, "## Spine focus") {
		t.Fatalf("Pack should omit spine focus section without context:\n%s", resp.Pack)
	}
}

func TestBuildSessionPackIncludesSpineFocus(t *testing.T) {
	ctx := &opscontext.File{
		Focus:      opscontext.Focus{Headline: "Stabilize satellite", Blocker: "D10 blocked"},
		Deployment: opscontext.Deployment{ActiveTrack: "build", Phase: "P3"},
		Milestones: []opscontext.Milestone{
			{ID: "m1", Label: "Milestone One", Status: "done"},
		},
	}
	req := PackRequest{PackSize: "full", Track: "build", Lane: "governance", SessionID: "sess-1", ProgramID: "prog-1", PhaseID: "phase-1"}

	resp := BuildSessionPack(ctx, nil, "", "", "", req)

	if resp.PackSize != "full" || resp.Track != "build" || resp.Lane != "governance" {
		t.Fatalf("resp = %+v", resp)
	}
	if !strings.Contains(resp.Pack, "Stabilize satellite") {
		t.Fatalf("Pack missing headline:\n%s", resp.Pack)
	}
	if !strings.Contains(resp.Pack, "D10 blocked") {
		t.Fatalf("Pack missing blocker:\n%s", resp.Pack)
	}
	if !strings.Contains(resp.Pack, "## Milestones (snapshot)") || !strings.Contains(resp.Pack, "Milestone One") {
		t.Fatalf("Pack missing milestones snapshot for full pack size:\n%s", resp.Pack)
	}
	if !strings.Contains(resp.Pack, "session_id: sess-1") || !strings.Contains(resp.Pack, "program_id: prog-1") || !strings.Contains(resp.Pack, "phase_id: phase-1") {
		t.Fatalf("Pack missing session binding fields:\n%s", resp.Pack)
	}
}

func TestBuildSessionPackCompactPackOmitsMilestonesSnapshot(t *testing.T) {
	ctx := &opscontext.File{
		Focus:      opscontext.Focus{Headline: "Headline"},
		Milestones: []opscontext.Milestone{{ID: "m1", Label: "Should not appear", Status: "done"}},
	}
	resp := BuildSessionPack(ctx, nil, "", "", "", PackRequest{PackSize: "compact"})

	if strings.Contains(resp.Pack, "Should not appear") {
		t.Fatalf("compact pack should not include the milestones snapshot:\n%s", resp.Pack)
	}
}

func TestBuildSessionPackSummarizesMatrixReachability(t *testing.T) {
	matrices := []probe.MatrixResponse{
		{
			Environment: "stg",
			Targets: []probe.Target{
				{ID: "a", Reachability: probe.ReachOK},
				{ID: "b", Reachability: probe.ReachOK},
				{ID: "c", Reachability: probe.ReachFail},
				{ID: "d", Reachability: probe.ReachDegraded},
			},
		},
	}
	resp := BuildSessionPack(nil, matrices, "", "", "", PackRequest{})

	if !strings.Contains(resp.Pack, "**stg**: ok 2 · fail 1 · degraded 1") {
		t.Fatalf("Pack missing matrix summary line:\n%s", resp.Pack)
	}
}

func TestBuildSessionPackIncludesClusterAndBaselineSections(t *testing.T) {
	resp := BuildSessionPack(nil, nil, "ok", "all green", "2026-07-01T00:00:00Z", PackRequest{})

	if !strings.Contains(resp.Pack, "## Cluster") || !strings.Contains(resp.Pack, "all green") {
		t.Fatalf("Pack missing cluster section:\n%s", resp.Pack)
	}
	if !strings.Contains(resp.Pack, "## Session baseline") || !strings.Contains(resp.Pack, "2026-07-01T00:00:00Z") {
		t.Fatalf("Pack missing baseline section:\n%s", resp.Pack)
	}
	if !resp.HasBaseline || resp.BaselineAt != "2026-07-01T00:00:00Z" {
		t.Fatalf("resp baseline fields = %+v", resp)
	}
}

func TestOrDash(t *testing.T) {
	if got := orDash(""); got != "—" {
		t.Fatalf("orDash(\"\") = %q, want em-dash", got)
	}
	if got := orDash("value"); got != "value" {
		t.Fatalf("orDash(value) = %q", got)
	}
}
