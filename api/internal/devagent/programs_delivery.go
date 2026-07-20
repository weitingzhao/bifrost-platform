package devagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/weitingzhao/bifrost-platform/api/internal/operatequeue"
)

type ProgramDetailBoardResponse struct {
	Program        ProgramSummary       `json:"program"`
	Phases         []PhaseDetailBoard   `json:"phases"`
	Bridge         BridgeConfig         `json:"bridge,omitempty"`
	Active         bool                 `json:"active"`
	AgentSessions  []AgentSessionRecord `json:"agent_sessions,omitempty"`
	PostCompletion *PostCompletionState `json:"post_completion,omitempty"`
	PendingItems   []PostCompletionItem `json:"pending_post_completion_items,omitempty"`
}

type PhaseDetailBoard struct {
	ID             string               `json:"id"`
	Title          string               `json:"title"`
	Status         string               `json:"status"`
	SignedOff      bool                 `json:"signed_off"`
	SignedOffAt    string               `json:"signed_off_at,omitempty"`
	SignedOffBy    string               `json:"signed_off_by,omitempty"`
	VerifyCmd      string               `json:"verify_cmd,omitempty"`
	Acceptance     []string             `json:"acceptance,omitempty"`
	DependsOn      []string             `json:"depends_on,omitempty"`
	SignOff        *PhaseSignOffConfig  `json:"sign_off,omitempty"`
	AgentSession   *AgentSessionConfig  `json:"agent_session,omitempty"`
	Progress       *PhaseProgressRecord `json:"progress,omitempty"`
	RenderedPrompt string               `json:"rendered_prompt,omitempty"`
	SkillInjected  bool                 `json:"skill_injected,omitempty"`
}

type PhaseSignoffRequest struct {
	SignedOffBy string `json:"signed_off_by,omitempty"`
	SignedOffAt string `json:"signed_off_at,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

type PhaseProgressRequest struct {
	Status       string `json:"status"`
	Summary      string `json:"summary,omitempty"`
	VerifyPassed bool   `json:"verify_passed"`
	// SessionID required — must match archived session program/phase.
	SessionID string `json:"session_id"`
}

type ProgramCompleteRequest struct {
	NewCapabilities   []string                    `json:"new_capabilities,omitempty"`
	NewRisks          []string                    `json:"new_risks,omitempty"`
	OperateQueueItems []OperateQueueItemBlueprint `json:"operate_queue_items,omitempty"`
}

type PostCompletionDecisionRequest struct {
	Reason     string `json:"reason"`
	DecisionBy string `json:"decision_by,omitempty"`
}

type LaunchRequest struct {
	SessionPack string `json:"session_pack"`
	Track       string `json:"track,omitempty"`
	Lane        string `json:"lane,omitempty"`
	Intent      string `json:"intent,omitempty"`
	ProgramID   string `json:"program_id,omitempty"`
	Model       string `json:"model,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
}

type LaunchResponse struct {
	AgentID   string `json:"agent_id,omitempty"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

type SessionStopRequest struct {
	ProgramID     string `json:"program_id,omitempty"`
	PhaseID       string `json:"phase_id,omitempty"`
	CursorAgentID string `json:"cursor_agent_id,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Track         string `json:"track,omitempty"`
	Lane          string `json:"lane,omitempty"`
	Intent        string `json:"intent,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
}

func (h *Handler) countSignedPhases(rt *programRuntime) int {
	n := 0
	for _, bp := range rt.blueprint.Phases {
		if h.phaseSignoffRecordLocked(rt, bp.ID) != nil {
			n++
		}
	}
	return n
}

func (h *Handler) isPhaseSigned(rt *programRuntime, phaseID string) bool {
	return h.phaseSignoffRecordLocked(rt, phaseID) != nil
}

func (h *Handler) phaseSignoffRecord(rt *programRuntime, phaseID string) *PhaseSignOffRecord {
	return h.phaseSignoffRecordLocked(rt, phaseID)
}

func (h *Handler) programDetailBoardResponse(programID string, rt *programRuntime) ProgramDetailBoardResponse {
	phases := make([]PhaseDetailBoard, len(rt.blueprint.Phases))
	statusByID := make(map[string]PhaseStatus, len(rt.phases))
	for _, p := range rt.phases {
		statusByID[p.ID] = p.Status
	}
	progressByID := map[string]*PhaseProgressRecord{}
	if rt.state != nil {
		for i := range rt.state.PhaseProgress {
			p := rt.state.PhaseProgress[i]
			progressByID[p.PhaseID] = &rt.state.PhaseProgress[i]
		}
	}
	for i, bp := range rt.blueprint.Phases {
		st := string(parsePhaseStatus(bp.Status))
		if runtimeSt, ok := statusByID[bp.ID]; ok {
			st = string(runtimeSt)
		}
		rec := h.phaseSignoffRecord(rt, bp.ID)
		detail := PhaseDetailBoard{
			ID:             bp.ID,
			Title:          bp.Title,
			Status:         st,
			VerifyCmd:      bp.VerifyCmd,
			Acceptance:     bp.Acceptance,
			DependsOn:      bp.DependsOn,
			SignOff:        bp.SignOff,
			AgentSession:   bp.AgentSession,
			Progress:       progressByID[bp.ID],
			RenderedPrompt: promptForPhase(rt.blueprint, bp.ID),
			SkillInjected:  skillFileLoaded(rt.blueprint.Workspace, rt.blueprint.SkillPath),
		}
		if rec != nil {
			detail.SignedOff = true
			detail.SignedOffAt = rec.SignedOffAt
			detail.SignedOffBy = rec.SignedOffBy
		}
		phases[i] = detail
	}
	resp := ProgramDetailBoardResponse{
		Program: h.buildProgramSummary(programID, rt),
		Phases:  phases,
		Active:  programID == h.activeProgramID,
	}
	if rt.blueprint.Workspace != "" {
		resp.Bridge = BridgeConfig{
			Workspace:   rt.blueprint.Workspace,
			Model:       rt.blueprint.Model,
			SkillPath:   rt.blueprint.SkillPath,
			SkillLoaded: skillFileLoaded(rt.blueprint.Workspace, rt.blueprint.SkillPath),
		}
	}
	if rt.state != nil {
		resp.AgentSessions = rt.state.AgentSessions
		resp.PostCompletion = rt.state.PostCompletion
	}
	pending, _ := h.store.LoadPendingPostCompletion()
	for _, item := range pending {
		if item.ProgramID == programID {
			resp.PendingItems = append(resp.PendingItems, item)
		}
	}
	return resp
}

func (h *Handler) HandleGetProgram(w http.ResponseWriter, r *http.Request) {
	programID := chi.URLParam(r, "programId")
	h.mu.Lock()
	defer h.mu.Unlock()

	rt, ok := h.runtimes[programID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}
	writeJSON(w, http.StatusOK, h.programDetailBoardResponse(programID, rt))
}

func (h *Handler) HandlePhaseSignoff(w http.ResponseWriter, r *http.Request) {
	programID := chi.URLParam(r, "programId")
	phaseID := chi.URLParam(r, "phaseId")
	var req PhaseSignoffRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.mu.Lock()
	defer h.mu.Unlock()

	rt, ok := h.runtimes[programID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}
	if !phaseExists(rt.blueprint, phaseID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "phase not found"})
		return
	}
	if programID == "vision" {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "vision program phases sign off via vision gate API (run gate first)",
		})
		return
	}
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
	}
	by := strings.TrimSpace(req.SignedOffBy)
	if by == "" {
		by = "owner"
	}
	if err := h.applyPhaseSignoffLocked(rt, phaseID, by, req.SignedOffAt, req.Notes); err != nil {
		if strings.Contains(err.Error(), "already signed off") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "RFC3339") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.persistRuntimeLocked(programID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h.programDetailBoardResponse(programID, rt))
}

func (h *Handler) HandlePhaseProgress(w http.ResponseWriter, r *http.Request) {
	programID := chi.URLParam(r, "programId")
	phaseID := chi.URLParam(r, "phaseId")
	var req PhaseProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if h.sessionStore != nil {
		if err := h.sessionStore.ValidateProgressHook(req.SessionID, programID, phaseID); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	rt, ok := h.runtimes[programID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}
	if !phaseExists(rt.blueprint, phaseID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "phase not found"})
		return
	}
	verifyCmd := phaseVerifyCmd(rt.blueprint, phaseID)
	if err := requireVerifyPassedForDone(req.Status, verifyCmd, req.VerifyPassed); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rec := PhaseProgressRecord{
		PhaseID: phaseID, Status: req.Status, Summary: req.Summary,
		VerifyPassed: req.VerifyPassed, UpdatedAt: now,
	}
	updated := false
	for i := range rt.state.PhaseProgress {
		if rt.state.PhaseProgress[i].PhaseID == phaseID {
			rt.state.PhaseProgress[i] = rec
			updated = true
			break
		}
	}
	if !updated {
		rt.state.PhaseProgress = append(rt.state.PhaseProgress, rec)
	}
	if err := h.persistRuntimeLocked(programID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Handler) HandleProgramComplete(w http.ResponseWriter, r *http.Request) {
	programID := chi.URLParam(r, "programId")
	var req ProgramCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	h.mu.Lock()

	rt, ok := h.runtimes[programID]
	if !ok {
		h.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	caps := req.NewCapabilities
	risks := req.NewRisks
	if len(caps) == 0 && rt.blueprint.PostCompletion != nil {
		caps = rt.blueprint.PostCompletion.NewCapabilities
	}
	if len(risks) == 0 && rt.blueprint.PostCompletion != nil {
		risks = rt.blueprint.PostCompletion.NewRisks
	}
	items := req.OperateQueueItems
	if len(items) == 0 && rt.blueprint.PostCompletion != nil {
		items = append(items, rt.blueprint.PostCompletion.OperateQueueItems...)
	}
	sourceLaneID := sourceLaneForRuntime(rt)
	defaultOperateLane := operateLaneForRuntime(rt)
	normalized := make([]OperateQueueItemBlueprint, 0, len(items))
	for _, item := range items {
		item = normalizeHandoffBlueprint(item, sourceLaneID, defaultOperateLane)
		if err := validateHandoffBlueprint(item); err != nil {
			h.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		normalized = append(normalized, item)
	}
	assessmentStatus := "not_assessed"
	if len(normalized) > 0 {
		assessmentStatus = "pending_review"
	}
	rt.state.PostCompletion = &PostCompletionState{
		SubmittedAt: now, AssessmentStatus: assessmentStatus,
		NewCapabilities: caps, NewRisks: risks,
		SuggestedItems:      buildDraftHandoffSuggestions(rt.blueprint, caps, risks),
		SuggestedAssessment: suggestedAssessment(rt.blueprint, caps, risks),
	}
	h.mu.Unlock()

	pending, err := h.store.LoadPendingPostCompletion()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var created []PostCompletionItem
	for _, it := range normalized {
		id := it.ID
		if id == "" {
			id = uuid.New().String()
		}
		item := PostCompletionItem{
			ID: id, ProgramID: programID, SourceLaneID: it.SourceLaneID,
			OperateLane: it.OperateLane, Title: it.Title, Description: it.Description,
			HandoffKind: it.HandoffKind, Reason: it.Reason, AgentTaskID: it.AgentTaskID,
			AcceptanceCriteria: it.AcceptanceCriteria, VerificationSteps: it.VerificationSteps,
			RiskLevel: it.RiskLevel, Owner: it.Owner, DueAt: it.DueAt,
			Status: "pending_review", CreatedAt: now,
		}
		pending = append(pending, item)
		created = append(created, item)
	}
	if err := h.store.SavePendingPostCompletion(pending); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.mu.Lock()
	if err := h.persistRuntimeLocked(programID); err != nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"program_id":    programID,
		"submitted_at":  now,
		"pending_items": created,
	})
}

func (h *Handler) HandleApprovePostCompletionItem(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	var req struct {
		ApprovedBy string `json:"approved_by,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	pending, err := h.store.LoadPendingPostCompletion()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	by := strings.TrimSpace(req.ApprovedBy)
	if by == "" {
		by = "owner"
	}
	var approved *PostCompletionItem
	for i := range pending {
		if pending[i].ID == itemID {
			if pending[i].Status != "pending_review" {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "item not pending review"})
				return
			}
			pending[i].Status = "approved"
			pending[i].ApprovedAt = now
			pending[i].ApprovedBy = by
			item := pending[i]
			approved = &item
			break
		}
	}
	if approved == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}
	if approved.OperateLane == "" {
		approved.OperateLane = h.operateLaneForProgram(approved.ProgramID)
	}

	resp := map[string]any{
		"id":          approved.ID,
		"program_id":  approved.ProgramID,
		"title":       approved.Title,
		"description": approved.Description,
		"status":      approved.Status,
		"created_at":  approved.CreatedAt,
		"approved_at": approved.ApprovedAt,
		"approved_by": approved.ApprovedBy,
	}

	if h.operateQueue != nil {
		queueItem, err := h.operateQueue.InjectFromApproval(r, operatequeue.ApprovalInjectParams{
			PendingID: approved.ID, ProgramID: approved.ProgramID,
			SourceLaneID: approved.SourceLaneID, OperateLane: approved.OperateLane,
			Title: approved.Title, Description: approved.Description,
			HandoffKind: approved.HandoffKind, Reason: approved.Reason,
			AgentTaskID: approved.AgentTaskID, AcceptanceCriteria: approved.AcceptanceCriteria,
			VerificationSteps: approved.VerificationSteps, RiskLevel: approved.RiskLevel,
			Owner: approved.Owner, DueAt: approved.DueAt, ApprovedBy: by,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		resp["operate_queue_item"] = queueItem
	}
	if err := h.store.SavePendingPostCompletion(pending); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.setProgramAssessmentStatus(approved.ProgramID, "approved", now, by, "")

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleRejectPostCompletionItem(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	var req PostCompletionDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reason required"})
		return
	}
	by := strings.TrimSpace(req.DecisionBy)
	if by == "" {
		by = "owner"
	}
	items, err := h.store.LoadPendingPostCompletion()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var rejected *PostCompletionItem
	for i := range items {
		if items[i].ID != itemID {
			continue
		}
		if items[i].Status != "pending_review" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "item not pending review"})
			return
		}
		items[i].Status = "rejected"
		items[i].RejectedAt = now
		items[i].RejectedBy = by
		items[i].DecisionNote = reason
		copy := items[i]
		rejected = &copy
		break
	}
	if rejected == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}
	if err := h.store.SavePendingPostCompletion(items); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.refreshProgramAssessmentFromItems(rejected.ProgramID, items, now, by)
	writeJSON(w, http.StatusOK, rejected)
}

func (h *Handler) HandleNoPostCompletionHandoff(w http.ResponseWriter, r *http.Request) {
	programID := chi.URLParam(r, "programId")
	var req PostCompletionDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reason required"})
		return
	}
	by := strings.TrimSpace(req.DecisionBy)
	if by == "" {
		by = "owner"
	}
	h.mu.Lock()
	_, exists := h.runtimes[programID]
	h.mu.Unlock()
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}
	items, err := h.store.LoadPendingPostCompletion()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range items {
		if items[i].ProgramID == programID && items[i].Status == "pending_review" {
			items[i].Status = "rejected"
			items[i].RejectedAt = now
			items[i].RejectedBy = by
			items[i].DecisionNote = "No handoff: " + reason
		}
	}
	if err := h.store.SavePendingPostCompletion(items); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.setProgramAssessmentStatus(programID, "no_handoff", now, by, reason)
	h.mu.Lock()
	resp := h.programDetailBoardResponse(programID, h.runtimes[programID])
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) setProgramAssessmentStatus(programID, status, at, by, noHandoffReason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rt := h.runtimes[programID]
	if rt == nil {
		return
	}
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
	}
	if rt.state.PostCompletion == nil {
		rt.state.PostCompletion = &PostCompletionState{}
	}
	rt.state.PostCompletion.AssessmentStatus = status
	rt.state.PostCompletion.AssessedAt = at
	rt.state.PostCompletion.AssessedBy = by
	rt.state.PostCompletion.NoHandoffReason = noHandoffReason
	_ = h.persistRuntimeLocked(programID)
}

func (h *Handler) refreshProgramAssessmentFromItems(programID string, items []PostCompletionItem, at, by string) {
	hasPending := false
	hasApproved := false
	for _, item := range items {
		if item.ProgramID != programID {
			continue
		}
		hasPending = hasPending || item.Status == "pending_review"
		hasApproved = hasApproved || item.Status == "approved"
	}
	status := "not_assessed"
	if hasApproved {
		status = "approved"
	} else if hasPending {
		status = "pending_review"
	}
	h.setProgramAssessmentStatus(programID, status, at, by, "")
}

func (h *Handler) OnOperateQueueExecution(queueItem operatequeue.Item) {
	items, err := h.store.LoadPendingPostCompletion()
	if err == nil {
		for i := range items {
			if items[i].ID == queueItem.PendingID {
				items[i].Status = "in_operate"
				items[i].ExecutionJobID = queueItem.ExecutionJobID
			}
		}
		_ = h.store.SavePendingPostCompletion(items)
	}
	h.setProgramAssessmentStatus(queueItem.ProgramID, "in_operate", queueItem.UpdatedAt, queueItem.ApprovedBy, "")
}

func (h *Handler) OnOperateQueueClosed(queueItem operatequeue.Item) {
	items, err := h.store.LoadPendingPostCompletion()
	if err == nil {
		for i := range items {
			if items[i].ID == queueItem.PendingID {
				items[i].Status = "closed"
				items[i].ExecutionJobID = queueItem.ExecutionJobID
				items[i].CompletionEvidence = append([]string(nil), queueItem.CompletionEvidence...)
			}
		}
		_ = h.store.SavePendingPostCompletion(items)
	}
	h.setProgramAssessmentStatus(queueItem.ProgramID, "closed", queueItem.ClosedAt, queueItem.ApprovedBy, "")
}

func (h *Handler) operateLaneForProgram(programID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	rt, ok := h.runtimes[programID]
	if !ok || rt.blueprint == nil || rt.blueprint.Metadata == nil {
		return ""
	}
	if lane, ok := rt.blueprint.Metadata["operate_lane"].(string); ok {
		return strings.TrimSpace(lane)
	}
	return ""
}

func sourceLaneForRuntime(rt *programRuntime) string {
	if rt == nil {
		return ""
	}
	if rt.state != nil && strings.TrimSpace(rt.state.LaneID) != "" {
		return strings.TrimSpace(rt.state.LaneID)
	}
	if rt.blueprint != nil && rt.blueprint.Metadata != nil {
		if lane, ok := rt.blueprint.Metadata["lane_id"].(string); ok {
			return strings.TrimSpace(lane)
		}
	}
	return ""
}

func operateLaneForRuntime(rt *programRuntime) string {
	if rt == nil || rt.blueprint == nil || rt.blueprint.Metadata == nil {
		return ""
	}
	if lane, ok := rt.blueprint.Metadata["operate_lane"].(string); ok {
		return strings.TrimSpace(lane)
	}
	return ""
}

func normalizeHandoffBlueprint(item OperateQueueItemBlueprint, sourceLaneID, defaultOperateLane string) OperateQueueItemBlueprint {
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	if item.SourceLaneID == "" {
		item.SourceLaneID = sourceLaneID
	}
	if item.OperateLane == "" {
		item.OperateLane = defaultOperateLane
	}
	if item.OperateLane == "" {
		item.OperateLane = "governance"
	}
	if item.HandoffKind == "" {
		item.HandoffKind = operatequeue.HandoffOneOff
	}
	if item.Reason == "" {
		item.Reason = item.Description
	}
	if item.Reason == "" {
		item.Reason = item.Title
	}
	if len(item.AcceptanceCriteria) == 0 {
		item.AcceptanceCriteria = []string{"The operational responsibility is implemented and observable."}
	}
	if len(item.VerificationSteps) == 0 {
		item.VerificationSteps = []string{"Verify the outcome and record completion evidence."}
	}
	if item.RiskLevel == "" {
		item.RiskLevel = operatequeue.RiskLow
	}
	return item
}

func validateHandoffBlueprint(item OperateQueueItemBlueprint) error {
	if item.Title == "" {
		return fmt.Errorf("title required")
	}
	return operatequeue.ValidateStructuredHandoff(operatequeue.EnqueueRequest{
		OperateLane: item.OperateLane, HandoffKind: item.HandoffKind, Reason: item.Reason,
		AgentTaskID: item.AgentTaskID, AcceptanceCriteria: item.AcceptanceCriteria,
		VerificationSteps: item.VerificationSteps, RiskLevel: item.RiskLevel, DueAt: item.DueAt,
	})
}

func suggestedAssessment(bp *ProgramBlueprint, capabilities, risks []string) string {
	text := strings.ToLower(strings.Join(append(append([]string{}, capabilities...), risks...), " "))
	if bp != nil {
		text += " " + strings.ToLower(bp.Title+" "+bp.Description)
	}
	if strings.Contains(text, "ui") || strings.Contains(text, "documentation") || strings.Contains(text, "docs") {
		if !strings.Contains(text, "deploy") && !strings.Contains(text, "runtime") && !strings.Contains(text, "database") {
			return "no_handoff"
		}
	}
	return "handoff"
}

func buildDraftHandoffSuggestions(bp *ProgramBlueprint, capabilities, risks []string) []OperateQueueItemBlueprint {
	text := strings.ToLower(strings.Join(append(append([]string{}, capabilities...), risks...), " "))
	if bp != nil {
		text += " " + strings.ToLower(bp.Title+" "+bp.Description)
	}
	sourceLane := ""
	operateLane := "governance"
	if bp != nil {
		sourceLane = sourceLaneForRuntime(&programRuntime{blueprint: bp})
		operateLane = operateLaneForRuntime(&programRuntime{blueprint: bp})
		if operateLane == "" {
			operateLane = "governance"
		}
	}
	base := OperateQueueItemBlueprint{
		SourceLaneID: sourceLane, OperateLane: operateLane, HandoffKind: operatequeue.HandoffOneOff,
		RiskLevel: operatequeue.RiskMedium,
	}
	switch {
	case strings.Contains(text, "secret") || strings.Contains(text, "certificate") || strings.Contains(text, "cert"):
		base.Title = "Establish rotation ownership"
		base.Reason = "New secret or certificate lifecycle requires an explicit operator owner."
		base.AcceptanceCriteria = []string{"Rotation owner and cadence are recorded."}
		base.VerificationSteps = []string{"Verify the active credential and rotation record."}
	case strings.Contains(text, "database") || strings.Contains(text, "schema") || strings.Contains(text, "migration"):
		base.Title = "Verify database migration operations"
		base.Reason = "Schema changes create post-delivery migration verification responsibility."
		base.AcceptanceCriteria = []string{"Migration is applied and backward-compatible checks pass."}
		base.VerificationSteps = []string{"Verify schema state and dependent workload health."}
	case strings.Contains(text, "pipeline") || strings.Contains(text, "ci/cd"):
		base.Title = "Establish pipeline recovery ownership"
		base.Reason = "The delivered pipeline requires an owned recovery and verification path."
		base.AgentTaskID = "deliver-stg-recover"
		base.AcceptanceCriteria = []string{"Recovery path is documented and a representative run succeeds."}
		base.VerificationSteps = []string{"Inspect the latest run and verify environment smoke checks."}
	case strings.Contains(text, "deploy") || strings.Contains(text, "runtime") || strings.Contains(text, "service"):
		base.Title = "Establish runtime observation"
		base.Reason = "The deployed runtime requires ongoing observation after Program completion."
		base.AgentTaskID = "ops"
		base.AcceptanceCriteria = []string{"Runtime health and ownership are visible in Agent Desk."}
		base.VerificationSteps = []string{"Verify health, alerts, and a completed observation pass."}
	default:
		return nil
	}
	return []OperateQueueItemBlueprint{base}
}

func (h *Handler) HandleListPendingPostCompletion(w http.ResponseWriter, _ *http.Request) {
	pending, err := h.store.LoadPendingPostCompletion()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var out []PostCompletionItem
	for _, item := range pending {
		if item.Status == "pending_review" {
			out = append(out, item)
		}
	}
	if out == nil {
		out = []PostCompletionItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) HandleLaunch(w http.ResponseWriter, r *http.Request) {
	var req LaunchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.SessionPack) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_pack required"})
		return
	}

	workspace := strings.TrimSpace(req.Workspace)
	model := strings.TrimSpace(req.Model)
	programID := strings.TrimSpace(req.ProgramID)

	h.mu.Lock()
	if workspace == "" && programID != "" {
		if rt, ok := h.runtimes[programID]; ok {
			workspace = rt.blueprint.Workspace
			if model == "" {
				model = rt.blueprint.Model
			}
		}
	}
	h.mu.Unlock()

	if workspace == "" {
		workspace = filepath.Join(h.repoRoot, "..")
	}
	if model == "" {
		model = "composer-2.5"
	}

	prompt := req.SessionPack
	if req.Track != "" || req.Lane != "" || req.Intent != "" {
		prompt = fmt.Sprintf("Track: %s\nLane: %s\nIntent: %s\n\n%s", req.Track, req.Lane, req.Intent, prompt)
	}

	args := []string{
		h.bridgeScriptPath(),
		"--prompt", prompt,
		"--workspace", workspace,
		"--model", model,
	}
	out, err := h.runBridgeCommand(args)
	sessionID := uuid.New().String()
	resp := LaunchResponse{
		SessionID: sessionID,
		Status:    "launched",
		Message:   string(out),
	}
	if err != nil {
		resp.Status = "failed"
		resp.Message = string(out) + "\n" + err.Error()
		writeJSON(w, http.StatusAccepted, resp)
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "[bridge] agent_id=") {
			resp.AgentID = strings.TrimPrefix(line, "[bridge] agent_id=")
		}
	}

	if programID != "" {
		h.mu.Lock()
		if rt, ok := h.runtimes[programID]; ok {
			if rt.state == nil {
				rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
			}
			rt.state.AgentSessions = append(rt.state.AgentSessions, AgentSessionRecord{
				ID: sessionID, ProgramID: programID, StartedAt: time.Now().UTC().Format(time.RFC3339),
				CursorAgentID: resp.AgentID, Track: req.Track, Lane: req.Lane, Intent: req.Intent,
				Summary: "briefing launch",
			})
			_ = h.persistRuntimeLocked(programID)
		}
		h.mu.Unlock()
	}

	writeJSON(w, http.StatusAccepted, resp)
}

func (h *Handler) HandleSessionStop(w http.ResponseWriter, r *http.Request) {
	var req SessionStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.ProgramID == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	rt, ok := h.runtimes[req.ProgramID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "program not found"})
		return
	}
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: req.ProgramID, History: []Job{}}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rt.state.AgentSessions = append(rt.state.AgentSessions, AgentSessionRecord{
		ID: uuid.New().String(), ProgramID: req.ProgramID, PhaseID: req.PhaseID,
		StartedAt: now, EndedAt: now, CursorAgentID: req.CursorAgentID,
		Summary: req.Summary, Track: req.Track, Lane: req.Lane, Intent: req.Intent,
	})
	_ = h.persistRuntimeLocked(req.ProgramID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func phaseExists(bp *ProgramBlueprint, phaseID string) bool {
	for _, p := range bp.Phases {
		if p.ID == phaseID {
			return true
		}
	}
	return false
}

func phaseVerifyCmd(bp *ProgramBlueprint, phaseID string) string {
	if bp == nil {
		return ""
	}
	for _, p := range bp.Phases {
		if p.ID == phaseID {
			return strings.TrimSpace(p.VerifyCmd)
		}
	}
	return ""
}

// isDoneLikeStatus treats "done" / "complete" as terminal phase progress.
func isDoneLikeStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "done" || s == "complete"
}

// requireVerifyPassedForDone enforces D-DU7: phases with verify_cmd cannot
// be marked done unless the agent reports verify_passed=true.
// The API does not execute verify_cmd (no remote shell).
func requireVerifyPassedForDone(status, verifyCmd string, verifyPassed bool) error {
	if !isDoneLikeStatus(status) {
		return nil
	}
	if strings.TrimSpace(verifyCmd) == "" {
		return nil
	}
	if verifyPassed {
		return nil
	}
	return fmt.Errorf("verify_passed required when phase has verify_cmd")
}
