package agentgovernance

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store     *remediation.JobStore
	overrides TrustOverrideStore
}

func NewHandler(store *remediation.JobStore) *Handler {
	_ = ensureAgentTasks()
	return &Handler{store: store, overrides: NewTrustOverrideStore()}
}

// UseTrustOverrideStore swaps the file store for another, e.g. the ConfigMap
// store when platform-api runs in the cluster.
func (h *Handler) UseTrustOverrideStore(s TrustOverrideStore) { h.overrides = s }

// TrustOverrideLocation names where this instance keeps the overrides.
func (h *Handler) TrustOverrideLocation() string { return h.overrides.Location() }

// listOverrides answers 503 itself when the store cannot be read: serving the
// matrix without the Owner's overrides would show a grant as missing.
func (h *Handler) listOverrides(w http.ResponseWriter, r *http.Request) (map[string]TrustOverride, bool) {
	o, err := h.overrides.List(r.Context())
	if err != nil {
		slog.Error("trust overrides unreadable", "store", h.overrides.Location(), "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "trust overrides unreadable: " + err.Error(),
			"store": h.overrides.Location(),
		})
		return nil, false
	}
	return o, true
}

func (h *Handler) HandlePerformance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ComputePerformance(h.store.List()))
}

// HandleListTasks returns YAML-backed agent task catalog (config/agent-tasks.yaml).
func (h *Handler) HandleListTasks(w http.ResponseWriter, _ *http.Request) {
	if err := ensureAgentTasks(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": "1",
		"tasks":   TaskCatalog(),
	})
}

func (h *Handler) HandleTrustMatrix(w http.ResponseWriter, r *http.Request) {
	overrides, ok := h.listOverrides(w, r)
	if !ok {
		return
	}
	jobs := h.store.List()
	raw := computeTrustMatrixRaw(jobs)
	resp := ApplyTrustOverrides(raw, overrides)
	resp.OverrideStore = h.overrides.Location()
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleTrustOverrides(w http.ResponseWriter, r *http.Request) {
	overrides, ok := h.listOverrides(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, TrustOverridesResponse{
		GeneratedAt: time.Now().UTC(),
		Overrides:   overrides,
		Store:       h.overrides.Location(),
	})
}

func (h *Handler) HandlePutTrustOverride(w http.ResponseWriter, r *http.Request) {
	skillID := strings.TrimSpace(chi.URLParam(r, "skill_id"))
	if !validSkillID(skillID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown skill_id"})
		return
	}
	var req TrustOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	current, ok := h.listOverrides(w, r)
	if !ok {
		return
	}
	jobs := h.store.List()
	raw := computeTrustMatrixRaw(jobs)
	merged := ApplyTrustOverrides(raw, current)
	var entry *TrustMatrixEntry
	for i := range merged.Entries {
		if merged.Entries[i].SkillID == skillID {
			entry = &merged.Entries[i]
			break
		}
	}
	if entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill not in catalog"})
		return
	}

	level := strings.TrimSpace(req.Level)
	reason := strings.TrimSpace(req.Reason)
	switch strings.TrimSpace(req.Action) {
	case "accept_promotion":
		if !entry.PromotionEligible || entry.SuggestedLevel == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skill not promotion eligible"})
			return
		}
		level = entry.SuggestedLevel
		if reason == "" {
			reason = "Owner accepted earned autonomy promotion"
		}
	case "apply_demotion":
		if !entry.DemotionTriggered || entry.SuggestedLevel == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skill has no demotion suggestion"})
			return
		}
		level = entry.SuggestedLevel
		if reason == "" {
			reason = "Owner applied demotion after failure spike"
		}
	}
	if level != "L0" && level != "L1" && level != "L2" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "level must be L0, L1, or L2"})
		return
	}
	appliedBy := strings.TrimSpace(req.AppliedBy)
	if appliedBy == "" {
		appliedBy = "operator"
	}
	if err := h.overrides.Put(r.Context(), TrustOverride{
		SkillID:   skillID,
		Level:     level,
		Reason:    reason,
		AppliedBy: appliedBy,
		AppliedAt: time.Now().UTC(),
	}); err != nil {
		slog.Error("trust override not saved", "skill_id", skillID, "store", h.overrides.Location(), "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "trust override not saved: " + err.Error(),
			"store": h.overrides.Location(),
		})
		return
	}
	after, ok := h.listOverrides(w, r)
	if !ok {
		return
	}
	final := ApplyTrustOverrides(raw, after)
	for i := range final.Entries {
		if final.Entries[i].SkillID == skillID {
			writeJSON(w, http.StatusOK, final.Entries[i])
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"skill_id": skillID, "level": level})
}

func validSkillID(id string) bool {
	for _, t := range TaskCatalog() {
		if t.ID == id {
			return true
		}
	}
	return false
}

func (h *Handler) HandleCapabilityMap(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ComputeCapabilityMap())
}

func (h *Handler) HandleSnapshot(w http.ResponseWriter, r *http.Request) {
	overrides, ok := h.listOverrides(w, r)
	if !ok {
		return
	}
	jobs := h.store.List()
	perf := ComputePerformance(jobs)
	rawTrust := computeTrustMatrixRaw(jobs)
	trust := ApplyTrustOverrides(rawTrust, overrides)
	capMap := ComputeCapabilityMap()
	brief := ComputeBriefing(jobs, trust)
	hermes := hermesConfigured()
	sources := []string{"remediation_jobs", "agent_task_catalog", "mcp_catalog", "owner_trust_overrides"}
	if hermes {
		sources = append(sources, "nous_hermes_optional")
	}
	note := "Flight Director KPIs sourced from remediation runner JobStore. Owner trust overrides persisted on platform-api. Hermes/GPU LLM path bypassed."
	writeJSON(w, http.StatusOK, SnapshotResponse{
		GeneratedAt:     time.Now().UTC(),
		HermesAvailable: hermes,
		DataSources:     sources,
		Performance:     perf,
		TrustMatrix:     trust,
		CapabilityMap:   capMap,
		Briefing:        brief,
		ProgramComplete: false,
		Note:            note,
	})
}

func hermesConfigured() bool {
	return strings.TrimSpace(os.Getenv("NOUS_HERMES_URL")) != "" ||
		strings.TrimSpace(os.Getenv("HERMES_GATEWAY_URL")) != ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func BuildSnapshot(jobs []remediation.Job) SnapshotResponse {
	perf := ComputePerformance(jobs)
	trust := ComputeTrustMatrix(jobs)
	capMap := ComputeCapabilityMap()
	return SnapshotResponse{
		GeneratedAt:     time.Now().UTC(),
		HermesAvailable: hermesConfigured(),
		DataSources:     []string{"remediation_jobs", "agent_task_catalog", "mcp_catalog"},
		Performance:     perf,
		TrustMatrix:     trust,
		CapabilityMap:   capMap,
		Briefing:        ComputeBriefing(jobs, trust),
	}
}
