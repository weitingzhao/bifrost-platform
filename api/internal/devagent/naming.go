package devagent

import (
	"fmt"
	"regexp"
	"strings"
)

const maxProgramIDLen = 40

var (
	programIDRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+){1,5}(--[a-z0-9-]+)?$`)
	// Stock P0 is allowed; new programs should start at P1 (documented in _schema.yaml).
	phaseNumRe      = regexp.MustCompile(`^P[0-9]+$`)
	phaseSemanticRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,20}$`)
	visionLegacyRe  = regexp.MustCompile(`^V[1-5]$|^S3$`)
	// network-governance has 8 phases NG1–NG8; deferred migration due to complexity.
	networkGovLegacyRe = regexp.MustCompile(`^NG[1-8]$`)
)

// NamingWarning is a non-fatal program/phase id convention finding.
type NamingWarning struct {
	ProgramID string `json:"program_id"`
	Field     string `json:"field"`
	Message   string `json:"message"`
}

func isArchivedStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "archived")
}

func isCompletedStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "completed")
}

func isClosedProgramStatus(status string) bool {
	return isArchivedStatus(status) || isCompletedStatus(status)
}

func isSelectableActiveStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "active")
}

// ValidateNewProgramID enforces conventions for newly created program ids.
// Stock ids may still contain a legacy `--` suffix; new ids must not.
func ValidateNewProgramID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("program id is empty")
	}
	if strings.Contains(id, "--") {
		return fmt.Errorf("program id %q must not contain --", id)
	}
	if len(id) > maxProgramIDLen {
		return fmt.Errorf("program id %q exceeds %d characters", id, maxProgramIDLen)
	}
	if !strings.Contains(id, "-") {
		return fmt.Errorf("program id %q must have at least two kebab-case segments", id)
	}
	if !programIDRe.MatchString(id) {
		return fmt.Errorf("program id %q does not match {domain}-{intent}[-{qualifier}] kebab-case", id)
	}
	return nil
}

func phaseIDAllowedForNewProgram(phaseID string) bool {
	if phaseID == "" {
		return false
	}
	if phaseNumRe.MatchString(phaseID) {
		return true
	}
	return phaseSemanticRe.MatchString(phaseID)
}

func queryFlagTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func phaseIDAliasMap(bp *ProgramBlueprint) map[string]string {
	if bp == nil {
		return nil
	}
	out := make(map[string]string)
	for _, p := range bp.Phases {
		for _, alias := range p.Aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" || alias == p.ID {
				continue
			}
			out[alias] = p.ID
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalPhaseID(bp *ProgramBlueprint, phaseID string) string {
	phaseID = strings.TrimSpace(phaseID)
	if bp == nil || phaseID == "" {
		return phaseID
	}
	for _, p := range bp.Phases {
		if p.ID == phaseID {
			return p.ID
		}
		for _, alias := range p.Aliases {
			if strings.TrimSpace(alias) == phaseID {
				return p.ID
			}
		}
	}
	if aliases := phaseIDAliasMap(bp); aliases != nil {
		if canon, ok := aliases[phaseID]; ok {
			return canon
		}
	}
	return phaseID
}

// RemapPhaseIDs rewrites persisted phase ids onto canonical blueprint ids.
// Returns true when any field changed.
func RemapPhaseIDs(state *ProgramStateRecord, aliases map[string]string) bool {
	if state == nil || len(aliases) == 0 {
		return false
	}
	changed := false
	remap := func(id string) string {
		id = strings.TrimSpace(id)
		if canon, ok := aliases[id]; ok && canon != "" && canon != id {
			changed = true
			return canon
		}
		return id
	}
	for i := range state.Phases {
		state.Phases[i].ID = remap(state.Phases[i].ID)
	}
	if state.ActiveJob != nil {
		state.ActiveJob.PhaseID = remap(state.ActiveJob.PhaseID)
	}
	for i := range state.History {
		state.History[i].PhaseID = remap(state.History[i].PhaseID)
	}
	for i := range state.PhaseSignOffs {
		state.PhaseSignOffs[i].PhaseID = remap(state.PhaseSignOffs[i].PhaseID)
	}
	for i := range state.PhaseProgress {
		state.PhaseProgress[i].PhaseID = remap(state.PhaseProgress[i].PhaseID)
	}
	for i := range state.AgentSessions {
		state.AgentSessions[i].PhaseID = remap(state.AgentSessions[i].PhaseID)
	}
	return changed
}

func phaseIDAllowed(programID, phaseID string) bool {
	if phaseID == "" {
		return false
	}
	if programID == "vision" && visionLegacyRe.MatchString(phaseID) {
		return true
	}
	if programID == "network-governance" && networkGovLegacyRe.MatchString(phaseID) {
		return true
	}
	if phaseNumRe.MatchString(phaseID) {
		return true
	}
	return phaseSemanticRe.MatchString(phaseID)
}

// CollectNamingWarnings reports convention issues for active programs only.
func CollectNamingWarnings(programs []*ProgramBlueprint) []NamingWarning {
	var out []NamingWarning
	for _, bp := range programs {
		if bp == nil || isArchivedStatus(bp.Status) || !strings.EqualFold(strings.TrimSpace(bp.Status), "active") {
			continue
		}
		id := strings.TrimSpace(bp.ID)
		if id == "" {
			out = append(out, NamingWarning{
				ProgramID: bp.ID,
				Field:     "id",
				Message:   "program id is empty",
			})
		} else if len(id) > maxProgramIDLen {
			out = append(out, NamingWarning{
				ProgramID: id,
				Field:     "id",
				Message:   fmt.Sprintf("program id %q exceeds %d characters", id, maxProgramIDLen),
			})
		} else if !programIDRe.MatchString(id) {
			out = append(out, NamingWarning{
				ProgramID: id,
				Field:     "id",
				Message:   fmt.Sprintf("program id %q does not match {domain}-{intent}[-{qualifier}] kebab-case", id),
			})
		}
		for _, p := range bp.Phases {
			if phaseIDAllowed(bp.ID, p.ID) {
				continue
			}
			out = append(out, NamingWarning{
				ProgramID: bp.ID,
				Field:     "phase_id",
				Message:   fmt.Sprintf("phase_id %q does not match P{n} or semantic slug", p.ID),
			})
		}
	}
	if out == nil {
		return []NamingWarning{}
	}
	return out
}
