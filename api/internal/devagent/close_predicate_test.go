package devagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestClosePredicatesCatalogStatusCompleted(t *testing.T) {
	sum := ProgramSummary{
		Status:                 "completed",
		Complete:               false,
		RequiresPostCompletion: true,
	}
	if !IsGatesComplete(sum) || !IsProgramCatalogComplete(sum) || !IsProgramSessionReleased(sum) {
		t.Fatalf("YAML status completed must close without scratch JSON: %+v", sum)
	}
	archived := ProgramSummary{Status: "archived", Complete: false, RequiresPostCompletion: true}
	if !IsProgramSessionReleased(archived) || !IsProgramCatalogComplete(archived) {
		t.Fatalf("archived must close: %+v", archived)
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

type programsListBody struct {
	Programs        []ProgramSummary    `json:"programs"`
	Collisions      []LiveLaneCollision `json:"live_lane_collisions"`
	NamingWarnings  []NamingWarning     `json:"naming_warnings"`
}

func TestHandleProgramsEmptyLiveLaneCollisions(t *testing.T) {
	h := &Handler{
		runtimes: map[string]*programRuntime{
			"a": {
				blueprint: &ProgramBlueprint{
					ID: "a", Title: "A",
					Delivery: &DeliveryConfig{BoardVisible: true},
					Metadata: map[string]interface{}{"lane_id": "console-api"},
				},
				state: &ProgramStateRecord{ProgramID: "a", LaneID: "console-api"},
			},
		},
	}
	rec := httptest.NewRecorder()
	h.HandlePrograms(rec, httptest.NewRequest(http.MethodGet, "/api/v1/programs?board=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body programsListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Collisions == nil {
		t.Fatal("live_lane_collisions must be [] not null")
	}
	if len(body.Collisions) != 0 {
		t.Fatalf("collisions=%+v", body.Collisions)
	}
	if len(body.Programs) != 1 {
		t.Fatalf("programs=%d", len(body.Programs))
	}
}

func TestHandleProgramsBoardIncludesPhases(t *testing.T) {
	h := &Handler{
		runtimes: map[string]*programRuntime{
			"trade-iv-radar": {
				blueprint: &ProgramBlueprint{
					ID: "trade-iv-radar", Title: "IV Radar", Status: "active",
					Delivery: &DeliveryConfig{BoardVisible: true},
					Metadata: map[string]interface{}{"lane_id": "trade-iv-radar"},
					Phases: []PhaseBlueprint{
						{ID: "P1", Title: "Nav", SignOff: &PhaseSignOffConfig{Required: true}},
						{ID: "P2", Title: "Data", SignOff: &PhaseSignOffConfig{Required: true}},
						{ID: "P3", Title: "UI", SignOff: &PhaseSignOffConfig{Required: true}},
						{ID: "P4", Title: "QA", SignOff: &PhaseSignOffConfig{Required: false}},
					},
				},
				phases: []Phase{
					{ID: "P1", Status: PhaseDone},
					{ID: "P2", Status: PhaseDone},
					{ID: "P3", Status: PhaseDone},
					{ID: "P4", Status: PhaseDone},
				},
				state: &ProgramStateRecord{ProgramID: "trade-iv-radar", LaneID: "trade-iv-radar"},
			},
		},
	}
	rec := httptest.NewRecorder()
	h.HandlePrograms(rec, httptest.NewRequest(http.MethodGet, "/api/v1/programs?board=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body programsListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Programs) != 1 {
		t.Fatalf("programs=%d", len(body.Programs))
	}
	p := body.Programs[0]
	if p.PhaseCount != 4 {
		t.Fatalf("phase_count=%d", p.PhaseCount)
	}
	if len(p.Phases) != 4 {
		t.Fatalf("phases len=%d want 4", len(p.Phases))
	}
	if p.Phases[0].ID != "P1" || p.Phases[0].Title == "" {
		t.Fatalf("phase0=%+v", p.Phases[0])
	}
	if p.Phases[0].Status != string(PhaseDone) {
		t.Fatalf("phase0 status=%s", p.Phases[0].Status)
	}

	// Non-board list omits phases.
	rec2 := httptest.NewRecorder()
	h.HandlePrograms(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/programs", nil))
	var body2 programsListBody
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatal(err)
	}
	if len(body2.Programs) != 1 {
		t.Fatalf("programs=%d", len(body2.Programs))
	}
	if len(body2.Programs[0].Phases) != 0 {
		t.Fatalf("non-board should omit phases, got %d", len(body2.Programs[0].Phases))
	}
}

func TestHandleProgramsReportsCollisionsDespiteLaneFilter(t *testing.T) {
	h := &Handler{
		runtimes: map[string]*programRuntime{
			"a": {
				blueprint: &ProgramBlueprint{
					ID: "a", Title: "A",
					Delivery: &DeliveryConfig{BoardVisible: true},
					Metadata: map[string]interface{}{"lane_id": "console-api"},
				},
				state: &ProgramStateRecord{ProgramID: "a", LaneID: "console-api"},
			},
			"b": {
				blueprint: &ProgramBlueprint{
					ID: "b", Title: "B",
					Delivery: &DeliveryConfig{BoardVisible: true},
					Metadata: map[string]interface{}{"lane_id": "console-api"},
				},
				state: &ProgramStateRecord{ProgramID: "b", LaneID: "console-api"},
			},
			"c": {
				blueprint: &ProgramBlueprint{
					ID: "c", Title: "C",
					Delivery: &DeliveryConfig{BoardVisible: true},
					Metadata: map[string]interface{}{"lane_id": "other"},
				},
				state: &ProgramStateRecord{ProgramID: "c", LaneID: "other"},
			},
		},
	}
	rec := httptest.NewRecorder()
	h.HandlePrograms(rec, httptest.NewRequest(http.MethodGet, "/api/v1/programs?board=1&lane_id=other", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body programsListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Programs) != 1 || body.Programs[0].ID != "c" {
		t.Fatalf("filtered programs=%+v", body.Programs)
	}
	if len(body.Collisions) != 1 || body.Collisions[0].LaneID != "console-api" || len(body.Collisions[0].ProgramIDs) != 2 {
		t.Fatalf("collisions=%+v", body.Collisions)
	}
}
