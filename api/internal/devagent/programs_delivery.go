package devagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
	mech := ""
	if rt.blueprint.Delivery != nil {
		mech = rt.blueprint.Delivery.SignOffMechanism
	}
	for _, bp := range rt.blueprint.Phases {
		if h.isPhaseSigned(rt, bp.ID, mech) {
			n++
		}
	}
	return n
}

func (h *Handler) isPhaseSigned(rt *programRuntime, phaseID, mechanism string) bool {
	if mechanism == "vision_gate" {
		at := h.visionGateSignedAt(phaseID)
		return at != ""
	}
	if rt.state == nil {
		return false
	}
	for _, s := range rt.state.PhaseSignOffs {
		if s.PhaseID == phaseID && s.SignedOffAt != "" {
			return true
		}
	}
	return false
}

func (h *Handler) phaseSignoffRecord(rt *programRuntime, phaseID, mechanism string) *PhaseSignOffRecord {
	if mechanism == "vision_gate" {
		at := h.visionGateSignedAt(phaseID)
		if at == "" {
			return nil
		}
		return &PhaseSignOffRecord{PhaseID: phaseID, SignedOffAt: at}
	}
	if rt.state == nil {
		return nil
	}
	for i := range rt.state.PhaseSignOffs {
		if rt.state.PhaseSignOffs[i].PhaseID == phaseID {
			return &rt.state.PhaseSignOffs[i]
		}
	}
	return nil
}

type visionSignoffFile struct {
	SignedAt string `json:"signed_at"`
	SignedBy string `json:"signed_by"`
}

func (h *Handler) visionGateSignedAt(phaseID string) string {
	fileByPhase := map[string]string{
		"V1":  "vision_v1_gate.json",
		"S3":  "vision_s3_gate.json",
		"V2":  "vision_v2_gate.json",
		"V3":  "vision_v3_gate.json",
		"V4":  "vision_v4_gate.json",
		"V5":  "vision_v5_gate.json",
	}
	fname, ok := fileByPhase[phaseID]
	if !ok {
		return ""
	}
	path := filepath.Join(h.configDir, fname)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var wrapper struct {
		Signoff *visionSignoffFile `json:"signoff"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return ""
	}
	if wrapper.Signoff == nil {
		return ""
	}
	return wrapper.Signoff.SignedAt
}

func (h *Handler) programDetailBoardResponse(programID string, rt *programRuntime) ProgramDetailBoardResponse {
	mech := ""
	if rt.blueprint.Delivery != nil {
		mech = rt.blueprint.Delivery.SignOffMechanism
	}
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
		rec := h.phaseSignoffRecord(rt, bp.ID, mech)
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
	mech := "api"
	if rt.blueprint.Delivery != nil && rt.blueprint.Delivery.SignOffMechanism != "" {
		mech = rt.blueprint.Delivery.SignOffMechanism
	}
	if mech == "vision_gate" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "vision_gate sign-off uses vision API endpoints"})
		return
	}
	if !phaseExists(rt.blueprint, phaseID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "phase not found"})
		return
	}
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
	}
	signedAt := strings.TrimSpace(req.SignedOffAt)
	if signedAt != "" {
		if _, err := time.Parse(time.RFC3339, signedAt); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signed_off_at must be RFC3339"})
			return
		}
	} else {
		signedAt = time.Now().UTC().Format(time.RFC3339)
	}
	by := strings.TrimSpace(req.SignedOffBy)
	if by == "" {
		by = "owner"
	}
	updated := false
	for i := range rt.state.PhaseSignOffs {
		if rt.state.PhaseSignOffs[i].PhaseID == phaseID {
			if rt.state.PhaseSignOffs[i].SignedOffAt != "" {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "phase already signed off"})
				return
			}
			rt.state.PhaseSignOffs[i].SignedOffAt = signedAt
			rt.state.PhaseSignOffs[i].SignedOffBy = by
			rt.state.PhaseSignOffs[i].Notes = req.Notes
			updated = true
			break
		}
	}
	if !updated {
		rt.state.PhaseSignOffs = append(rt.state.PhaseSignOffs, PhaseSignOffRecord{
			PhaseID: phaseID, SignedOffAt: signedAt, SignedOffBy: by, Notes: req.Notes,
		})
	}
	for i := range rt.phases {
		if rt.phases[i].ID == phaseID {
			rt.phases[i].Status = PhaseDone
			rt.phases[i].CompletedAt = signedAt
		}
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
	defer h.mu.Unlock()

	rt, ok := h.runtimes[programID]
	if !ok {
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
	found := false
	for i := range pending {
		if pending[i].ID == itemID {
			if pending[i].Status != "pending_review" {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "item not pending review"})
				return
			}
			pending[i].Status = "approved"
			pending[i].ApprovedAt = now
			pending[i].ApprovedBy = by
			found = true
			if err := h.store.SavePendingPostCompletion(pending); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, pending[i])
			return
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
	}
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
