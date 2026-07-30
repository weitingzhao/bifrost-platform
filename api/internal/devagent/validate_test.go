package devagent

import (
	"strings"
	"testing"
)

func gatePhase(id string) PhaseBlueprint {
	return PhaseBlueprint{
		ID:      id,
		Title:   id,
		SignOff: &PhaseSignOffConfig{Required: true},
	}
}

func workPhase(id string) PhaseBlueprint {
	return PhaseBlueprint{
		ID:      id,
		Title:   id,
		SignOff: &PhaseSignOffConfig{Required: false},
	}
}

func legacyGatePhase(id string) PhaseBlueprint {
	return PhaseBlueprint{ID: id, Title: id} // SignOff nil → gate
}

func TestValidateGateRules_CompliantMultiGate(t *testing.T) {
	bp := &ProgramBlueprint{
		ID: "ok-multi",
		Phases: []PhaseBlueprint{
			workPhase("w1"),
			gatePhase("g1"),
			workPhase("w2"),
			gatePhase("g2"),
		},
	}
	if got := validateGateRules(bp); len(got) != 0 {
		t.Fatalf("expected no warnings, got %v", got)
	}
}

func TestValidateGateRules_CompliantSoleGateLast(t *testing.T) {
	bp := &ProgramBlueprint{
		ID: "ok-sole",
		Phases: []PhaseBlueprint{
			workPhase("w1"),
			workPhase("w2"),
			gatePhase("g1"),
		},
	}
	if got := validateGateRules(bp); len(got) != 0 {
		t.Fatalf("expected no warnings, got %v", got)
	}
}

func TestValidateGateRules_LegacyNilSignOffCountsAsGate(t *testing.T) {
	bp := &ProgramBlueprint{
		ID: "ok-legacy",
		Phases: []PhaseBlueprint{
			legacyGatePhase("p1"),
			legacyGatePhase("p2"),
		},
	}
	if got := validateGateRules(bp); len(got) != 0 {
		t.Fatalf("expected no warnings, got %v", got)
	}
}

func TestValidateGateRules_NoGates(t *testing.T) {
	bp := &ProgramBlueprint{
		ID: "bad-none",
		Phases: []PhaseBlueprint{
			workPhase("w1"),
			workPhase("w2"),
		},
	}
	got := validateGateRules(bp)
	if len(got) != 1 || !strings.Contains(got[0], "at least one gate") {
		t.Fatalf("expected at-least-one-gate warning, got %v", got)
	}
}

func TestValidateGateRules_SoleGateNotLast(t *testing.T) {
	bp := &ProgramBlueprint{
		ID: "bad-sole-mid",
		Phases: []PhaseBlueprint{
			gatePhase("g1"),
			workPhase("w1"),
		},
	}
	got := validateGateRules(bp)
	if len(got) == 0 {
		t.Fatal("expected warnings")
	}
	joined := strings.Join(got, " | ")
	if !strings.Contains(joined, "sole gate") {
		t.Fatalf("expected sole-gate-must-be-last warning, got %v", got)
	}
	if !strings.Contains(joined, "after last gate") {
		t.Fatalf("expected trailing-work warning, got %v", got)
	}
}

func TestValidateGateRules_WorkAfterLastGate(t *testing.T) {
	bp := &ProgramBlueprint{
		ID: "bad-trailing",
		Phases: []PhaseBlueprint{
			gatePhase("g1"),
			gatePhase("g2"),
			workPhase("w-tail"),
		},
	}
	got := validateGateRules(bp)
	if len(got) != 1 || !strings.Contains(got[0], "after last gate") {
		t.Fatalf("expected trailing-work warning, got %v", got)
	}
}

func TestValidateGateRules_EmptyPhases(t *testing.T) {
	bp := &ProgramBlueprint{ID: "empty", Phases: nil}
	got := validateGateRules(bp)
	if len(got) != 1 || !strings.Contains(got[0], "no phases") {
		t.Fatalf("expected no-phases warning, got %v", got)
	}
}

func TestValidateGateRules_NilBlueprint(t *testing.T) {
	if got := validateGateRules(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestCountSignOffRequiredPhases(t *testing.T) {
	bp := &ProgramBlueprint{
		Phases: []PhaseBlueprint{
			workPhase("w1"),
			gatePhase("g1"),
			legacyGatePhase("g2"),
			workPhase("w2"),
			gatePhase("g3"),
		},
	}
	if n := countSignOffRequiredPhases(bp); n != 3 {
		t.Fatalf("countSignOffRequiredPhases = %d, want 3", n)
	}
	if n := countSignOffRequiredPhases(nil); n != 0 {
		t.Fatalf("nil blueprint count = %d, want 0", n)
	}
}
