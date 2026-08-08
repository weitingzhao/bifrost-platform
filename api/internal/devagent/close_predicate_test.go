package devagent

import "testing"

func TestClosePredicatesNoPostCompletionAutoGraduate(t *testing.T) {
	sum := ProgramSummary{Complete: true, RequiresPostCompletion: false}
	if !IsGatesComplete(sum) || !IsProgramCatalogComplete(sum) || !IsProgramSessionReleased(sum) {
		t.Fatalf("no post_completion + gates done should auto-graduate: %+v", sum)
	}
}

func TestClosePredicatesRequiresPostCompletionEmptyAssessment(t *testing.T) {
	sum := ProgramSummary{Complete: true, RequiresPostCompletion: true}
	if IsProgramCatalogComplete(sum) || IsProgramSessionReleased(sum) {
		t.Fatalf("req_pc + empty assessment must stay open: %+v", sum)
	}
}

func TestClosePredicatesNoHandoff(t *testing.T) {
	sum := ProgramSummary{
		Complete:                 true,
		RequiresPostCompletion:   true,
		AssessmentStatus:         "no_handoff",
	}
	if !IsProgramCatalogComplete(sum) || !IsProgramSessionReleased(sum) {
		t.Fatalf("no_handoff should catalog+session close: %+v", sum)
	}
}

func TestClosePredicatesInOperateSessionOnly(t *testing.T) {
	sum := ProgramSummary{
		Complete:               true,
		RequiresPostCompletion: true,
		AssessmentStatus:       "in_operate",
	}
	if IsProgramCatalogComplete(sum) {
		t.Fatal("in_operate is not catalogComplete")
	}
	if !IsProgramSessionReleased(sum) {
		t.Fatal("in_operate must sessionRelease")
	}
}

func TestClosePredicatesPendingReviewNotReleased(t *testing.T) {
	sum := ProgramSummary{
		Complete:               true,
		RequiresPostCompletion: true,
		AssessmentStatus:       "pending_review",
	}
	if IsProgramCatalogComplete(sum) || IsProgramSessionReleased(sum) {
		t.Fatalf("pending_review stays in Active Session: %+v", sum)
	}
}

func TestClosePredicatesApprovedSessionReleased(t *testing.T) {
	sum := ProgramSummary{
		Complete:               true,
		RequiresPostCompletion: true,
		AssessmentStatus:       "approved",
	}
	if IsProgramCatalogComplete(sum) {
		t.Fatal("approved is not catalogComplete")
	}
	if !IsProgramSessionReleased(sum) {
		t.Fatal("approved must sessionRelease")
	}
}

func TestIsGatesCompleteFallsBackToSignedCounts(t *testing.T) {
	sum := ProgramSummary{
		Complete:             false,
		Signed:               4,
		SignOffRequiredCount: 4,
		PhaseCount:           6,
		PhasesDone:           6,
	}
	if !IsGatesComplete(sum) {
		t.Fatal("signed==gates must count as gatesComplete without Complete flag")
	}
	noGates := ProgramSummary{Complete: false, PhaseCount: 3, PhasesDone: 3}
	if !IsGatesComplete(noGates) {
		t.Fatal("no gates + all phases done must be gatesComplete")
	}
}

func TestClosePredicatesGatesIncomplete(t *testing.T) {
	sum := ProgramSummary{
		Complete:               false,
		RequiresPostCompletion: false,
		AssessmentStatus:       "no_handoff",
	}
	if IsProgramCatalogComplete(sum) || IsProgramSessionReleased(sum) {
		t.Fatalf("incomplete gates never close: %+v", sum)
	}
}

func TestLiveLaneCollisionsDetectsDoubleLive(t *testing.T) {
	h := &Handler{
		runtimes: map[string]*programRuntime{
			"a": {
				blueprint: &ProgramBlueprint{ID: "a", Title: "A", Metadata: map[string]interface{}{"lane_id": "console-api"}},
				state:     &ProgramStateRecord{ProgramID: "a", LaneID: "console-api"},
			},
			"b": {
				blueprint: &ProgramBlueprint{ID: "b", Title: "B", Metadata: map[string]interface{}{"lane_id": "console-api"}},
				state:     &ProgramStateRecord{ProgramID: "b", LaneID: "console-api"},
			},
			"c": {
				blueprint: &ProgramBlueprint{ID: "c", Title: "C", Metadata: map[string]interface{}{"lane_id": "other"}},
				state:     &ProgramStateRecord{ProgramID: "c", LaneID: "other"},
			},
		},
	}
	got := h.liveLaneCollisionsLocked()
	if len(got) != 1 || got[0].LaneID != "console-api" || len(got[0].ProgramIDs) != 2 {
		t.Fatalf("collisions=%+v", got)
	}
}

func TestLiveLaneCollisionsIgnoresSessionReleased(t *testing.T) {
	h := &Handler{
		runtimes: map[string]*programRuntime{
			"closed": {
				blueprint: &ProgramBlueprint{
					ID: "closed", Title: "Closed",
					Metadata:       map[string]interface{}{"lane_id": "console-api"},
					PostCompletion: &PostCompletionBlueprint{NewCapabilities: []string{"x"}},
					Phases:         []PhaseBlueprint{gatePhase("P0")},
				},
				state: &ProgramStateRecord{
					ProgramID: "closed", LaneID: "console-api",
					PostCompletion: &PostCompletionState{AssessmentStatus: "no_handoff"},
					PhaseSignOffs: []PhaseSignOffRecord{
						{PhaseID: "P0", SignedOffAt: "2026-08-08T00:00:00Z", SignedOffBy: "owner"},
					},
				},
			},
			"live": {
				blueprint: &ProgramBlueprint{ID: "live", Title: "Live", Metadata: map[string]interface{}{"lane_id": "console-api"}},
				state:     &ProgramStateRecord{ProgramID: "live", LaneID: "console-api"},
			},
		},
	}
	if got := h.liveLaneCollisionsLocked(); len(got) != 0 {
		t.Fatalf("released sibling must not collide: %+v", got)
	}
}
