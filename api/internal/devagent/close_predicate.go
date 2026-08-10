package devagent

import (
	"fmt"
	"sort"
	"strings"
)

// Close predicates — keep in sync with console/src/lib/briefing/programClose.ts
//
// gatesComplete = existing API `complete` (gates signed, else all phases done).
//
// catalogComplete (Delivery Board Complete band):
//   if requires_post_completion: assessment ∈ {no_handoff, closed}
//   else: gatesComplete
//
// sessionReleased (leave Active Session Doing):
//   if requires_post_completion: assessment ∈ {no_handoff, closed, approved, in_operate}
//   else: gatesComplete
//
// pending_review or empty assessment + requires_post_completion → NOT released.

var catalogCompleteAssessments = map[string]struct{}{
	"no_handoff": {},
	"closed":     {},
}

var sessionReleasedAssessments = map[string]struct{}{
	"no_handoff": {},
	"closed":     {},
	"approved":   {},
	"in_operate": {},
}

func normalizeAssessment(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// IsGatesComplete matches console isGatesComplete:
// Complete || (gates>0 && Signed==gates) || (no gates && PhasesDone==PhaseCount).
// Catalog status completed/archived is closed without requiring scratch JSON.
//
// Signed==0 falls back to PhasesSigned because Go ints are zero-valued when
// omitted; TS uses ?? so an explicit 0 does not fall back. Handler writes both
// fields to the same count — do not change the algorithm to "match" TS nil.
func IsGatesComplete(sum ProgramSummary) bool {
	if isClosedProgramStatus(sum.Status) {
		return true
	}
	if sum.Complete {
		return true
	}
	signed := sum.Signed
	if signed == 0 {
		signed = sum.PhasesSigned
	}
	gates := sum.SignOffRequiredCount
	if gates > 0 {
		return signed == gates
	}
	return sum.PhaseCount > 0 && sum.PhasesDone == sum.PhaseCount
}

// IsProgramCatalogComplete is Delivery Board Complete (D1).
func IsProgramCatalogComplete(sum ProgramSummary) bool {
	if isClosedProgramStatus(sum.Status) {
		return true
	}
	if !IsGatesComplete(sum) {
		return false
	}
	if !sum.RequiresPostCompletion {
		return true
	}
	_, ok := catalogCompleteAssessments[normalizeAssessment(sum.AssessmentStatus)]
	return ok
}

// IsProgramSessionReleased is true when the program may leave Active Session Doing (D3).
func IsProgramSessionReleased(sum ProgramSummary) bool {
	if isClosedProgramStatus(sum.Status) {
		return true
	}
	if !IsGatesComplete(sum) {
		return false
	}
	if !sum.RequiresPostCompletion {
		return true
	}
	_, ok := sessionReleasedAssessments[normalizeAssessment(sum.AssessmentStatus)]
	return ok
}

// LiveLaneBindError is returned when a second not-sessionReleased program would bind the same lane.
type LiveLaneBindError struct {
	LaneID            string
	BlockingProgramID string
}

func (e *LiveLaneBindError) Error() string {
	return fmt.Sprintf("lane %s already has live program %s", e.LaneID, e.BlockingProgramID)
}

// liveProgramBlockingLane returns a not-sessionReleased sibling on laneID (excludeProgramID skipped).
// Caller must hold h.mu.
func (h *Handler) liveProgramBlockingLane(laneID, excludeProgramID string) *ProgramSummary {
	lane := strings.TrimSpace(laneID)
	if lane == "" {
		return nil
	}
	exclude := strings.TrimSpace(excludeProgramID)
	for id, rt := range h.runtimes {
		if id == exclude {
			continue
		}
		sum := h.buildProgramSummary(id, rt)
		if strings.TrimSpace(sum.LaneID) != lane {
			continue
		}
		if !IsProgramSessionReleased(sum) {
			copy := sum
			return &copy
		}
	}
	return nil
}

func (h *Handler) errIfLiveLaneBindLocked(laneID, excludeProgramID string) error {
	blocker := h.liveProgramBlockingLane(laneID, excludeProgramID)
	if blocker == nil {
		return nil
	}
	return &LiveLaneBindError{LaneID: strings.TrimSpace(laneID), BlockingProgramID: blocker.ID}
}

// LiveLaneCollision is a D2 invariant break: >1 not-sessionReleased program on one lane.
type LiveLaneCollision struct {
	LaneID     string   `json:"lane_id"`
	ProgramIDs []string `json:"program_ids"`
}

// liveLaneCollisionsLocked scans loaded runtimes (YAML + persisted state). Caller must hold h.mu.
func (h *Handler) liveLaneCollisionsLocked() []LiveLaneCollision {
	byLane := map[string][]string{}
	for id, rt := range h.runtimes {
		if rt == nil || rt.blueprint == nil {
			continue
		}
		sum := h.buildProgramSummary(id, rt)
		lane := strings.TrimSpace(sum.LaneID)
		if lane == "" || IsProgramSessionReleased(sum) {
			continue
		}
		byLane[lane] = append(byLane[lane], id)
	}
	out := make([]LiveLaneCollision, 0)
	for lane, ids := range byLane {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		out = append(out, LiveLaneCollision{LaneID: lane, ProgramIDs: ids})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LaneID < out[j].LaneID })
	return out
}
