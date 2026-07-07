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
	Program        ProgramSummary        `json:"program"`
	Phases         []PhaseDetailBoard    `json:"phases"`
	Bridge         BridgeConfig          `json:"bridge,omitempty"`
	Active         bool                  `json:"active"`
	AgentSessions  []AgentSessionRecord  `json:"agent_sessions,omitempty"`
	PostCompletion *PostCompletionState  `json:"post_completion,omitempty"`
	PendingItems   []PostCompletionItem  `json:"pending_post_completion_items,omitempty"`
}

type PhaseDetailBoard struct {
	ID             string              `json:"id"`
	Title          string              `json:"title"`
	Status         string              `json:"status"`
	SignedOff      bool                `json:"signed_off"`
	SignedOffAt    string              `json:"signed_off_at,omitempty"`
	SignedOffBy    string              `json:"signed_off_by,omitempty"`
	VerifyCmd      string              `json:"verify_cmd,omitempty"`
	Acceptance     []string            `json:"acceptance,omitempty"`
	DependsOn      []string            `json:"depends_on,omitempty"`
	SignOff        *PhaseSignOffConfig `json:"sign_off,omitempty"`
	AgentSession   *AgentSessionConfig `json:"agent_session,omitempty"`
	Progress       *PhaseProgressRecord `json:"progress,omitempty"`
	RenderedPrompt string              `json:"rendered_prompt,omitempty"`
	SkillInjected  bool                `json:"skill_injected,omitempty"`
}

type PhaseSignoffRequest struct {
	SignedOffBy  string `json:"signed_off_by,omitempty"`
	SignedOffAt  string `json:"signed_off_at,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

type PhaseProgressRequest struct {
	Status       string `json:"status"`
	Summary      string `json:"summary,omitempty"`
	VerifyPassed bool   `json:"verify_passed"`
}

type ProgramCompleteRequest struct {
	NewCapabilities   []string `json:"new_capabilities,omitempty"`
	NewRisks          []string `json:"new_risks,omitempty"`
	OperateQueueItems []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
	} `json:"operate_queue_items,omitempty"`
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
		if item.ProgramID == programID && item.Status == "pending_review" {
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
	rt.state.PostCompletion = &PostCompletionState{
		SubmittedAt: now, NewCapabilities: caps, NewRisks: risks,
	}

	items := req.OperateQueueItems
	if len(items) == 0 && rt.blueprint.PostCompletion != nil {
		for _, b := range rt.blueprint.PostCompletion.OperateQueueItems {
			items = append(items, struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				Description string `json:"description,omitempty"`
			}{ID: b.ID, Title: b.Title, Description: b.Description})
		}
	}
	h.mu.Unlock()

	pending, err := h.store.LoadPendingPostCompletion()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var created []PostCompletionItem
	for _, it := range items {
		id := it.ID
		if id == "" {
			id = uuid.New().String()
		}
		item := PostCompletionItem{
			ID: id, ProgramID: programID, Title: it.Title, Description: it.Description,
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
		"program_id": programID,
		"submitted_at": now,
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
			if err := h.store.SavePendingPostCompletion(pending); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			break
		}
	}
	if approved == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}

	resp := map[string]any{
		"id":           approved.ID,
		"program_id":   approved.ProgramID,
		"title":        approved.Title,
		"description":  approved.Description,
		"status":       approved.Status,
		"created_at":   approved.CreatedAt,
		"approved_at":  approved.ApprovedAt,
		"approved_by":  approved.ApprovedBy,
	}

	if h.operateQueue != nil {
		queueItem, err := h.operateQueue.InjectFromApproval(r, operatequeue.ApprovalInjectParams{
			PendingID:   approved.ID,
			ProgramID:   approved.ProgramID,
			Title:       approved.Title,
			Description: approved.Description,
			Lane:        h.operateLaneForProgram(approved.ProgramID),
			ApprovedBy:  by,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		resp["operate_queue_item"] = queueItem
	}

	writeJSON(w, http.StatusOK, resp)
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
