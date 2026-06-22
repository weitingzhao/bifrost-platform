package driftproposal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store   *Store
	runner  *remediation.RunnerClient
	audit   *actuation.AuditLog
}

func NewHandler(audit *actuation.AuditLog) *Handler {
	return &Handler{
		store:  NewStore(),
		runner: remediation.NewRunnerClient(),
		audit:  audit,
	}
}

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	list := h.store.List()
	writeJSON(w, http.StatusOK, map[string]any{"proposals": list})
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, ok := h.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "proposal not found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if len(req.LayersFailed) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "layers_failed required"})
		return
	}
	if strings.TrimSpace(req.Summary) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "summary required"})
		return
	}

	principal := actuation.PrincipalFromContext(r.Context())
	now := time.Now().UTC()
	p := Proposal{
		ID:            newProposalID(),
		Status:        StatusPendingApproval,
		Host:          req.Host,
		PlatformAPI:   req.PlatformAPI,
		ReportSource:  req.ReportSource,
		LayersFailed:  req.LayersFailed,
		FindingsCount: req.FindingsCount,
		Summary:       req.Summary,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := h.store.Put(p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Record(r, "drift.proposal.create", p.ID, "pending_approval", fmt.Sprintf("layers=%v", req.LayersFailed))
	writeJSON(w, http.StatusCreated, p)
	_ = principal
}

func (h *Handler) HandleApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, ok := h.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "proposal not found"})
		return
	}
	if p.Status != StatusPendingApproval {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "proposal not pending approval"})
		return
	}

	principal := actuation.PrincipalFromContext(r.Context())
	prompt := buildAutofixPrompt(*p)
	runReq := remediation.StartRunnerRequest{
		Scope:  "nightly-drift-autofix",
		Actor:  principal.Name,
		Prompt: prompt,
	}

	job, err := h.runner.Start(r.Context(), runReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":  "remediation runner unavailable",
			"detail": err.Error(),
		})
		return
	}

	now := time.Now().UTC()
	p.Status = StatusRunning
	p.RemediationJobID = job.ID
	p.ApprovedBy = principal.Name
	p.ApprovedAt = &now
	p.UpdatedAt = now
	if err := h.store.Put(*p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.audit.Record(r, "drift.proposal.approve", id, "running", job.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"proposal": p, "remediation_job": job})
}

func (h *Handler) HandleReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, ok := h.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "proposal not found"})
		return
	}
	if p.Status != StatusPendingApproval {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "proposal not pending approval"})
		return
	}

	var req RejectRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	principal := actuation.PrincipalFromContext(r.Context())
	now := time.Now().UTC()
	p.Status = StatusRejected
	p.RejectedBy = principal.Name
	p.RejectedAt = &now
	p.RejectNote = strings.TrimSpace(req.Note)
	p.UpdatedAt = now
	if err := h.store.Put(*p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Record(r, "drift.proposal.reject", id, "rejected", p.RejectNote)
	writeJSON(w, http.StatusOK, p)
}

func buildAutofixPrompt(p Proposal) string {
	layers := strings.Join(p.LayersFailed, ", ")
	return fmt.Sprintf(
		"Nightly drift auto-fix (Owner approved proposal %s).\n\n"+
			"Layers failed: %s\nFindings count: %d\nReport: %s\n\n"+
			"### Drift excerpt\n%s\n\n"+
			"Apply safe fixes in bifrost-platform only (catalog paths, spine/catalog alignment, doc refs). "+
			"Create branch `agent/drift-%s`, commit with clear messages. "+
			"If `git` remote is configured, push branch and output `gh pr create` command or PR URL. "+
			"Do NOT run cluster destructive actions. Summarize files changed and PR steps for Owner.",
		p.ID,
		layers,
		p.FindingsCount,
		p.ReportSource,
		p.Summary,
		time.Now().UTC().Format("20060102"),
	)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
