package devagent

import "fmt"

// phaseIsGate reports whether a phase requires Owner sign-off.
// Nil SignOff (legacy default) or Required=true both count as gates.
func phaseIsGate(p PhaseBlueprint) bool {
	return p.SignOff == nil || p.SignOff.Required
}

// validateGateRules checks Owner gate design rules for a program blueprint.
// Returns human-readable warning strings (empty when compliant).
//
// Rules:
//  1. At least one gate phase (SignOff nil or Required true)
//  2. If exactly one gate, it must be the last phase
//  3. No work phases (Required false) after the last gate
func validateGateRules(bp *ProgramBlueprint) []string {
	if bp == nil {
		return nil
	}
	phases := bp.Phases
	if len(phases) == 0 {
		return []string{fmt.Sprintf("program %q: no phases defined", bp.ID)}
	}

	var gateIdxs []int
	for i, p := range phases {
		if phaseIsGate(p) {
			gateIdxs = append(gateIdxs, i)
		}
	}

	var warnings []string
	if len(gateIdxs) == 0 {
		warnings = append(warnings, fmt.Sprintf(
			"program %q: at least one gate phase required (sign_off.required true or omit sign_off)",
			bp.ID,
		))
		return warnings
	}

	lastGateIdx := gateIdxs[len(gateIdxs)-1]
	if len(gateIdxs) == 1 && lastGateIdx != len(phases)-1 {
		warnings = append(warnings, fmt.Sprintf(
			"program %q: sole gate phase %q must be the last phase",
			bp.ID, phases[lastGateIdx].ID,
		))
	}

	for i := lastGateIdx + 1; i < len(phases); i++ {
		if !phaseIsGate(phases[i]) {
			warnings = append(warnings, fmt.Sprintf(
				"program %q: work phase %q appears after last gate %q (trailing work has no Owner acceptance)",
				bp.ID, phases[i].ID, phases[lastGateIdx].ID,
			))
		}
	}

	return warnings
}
