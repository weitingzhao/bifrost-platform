package agentgovernance

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store    *remediation.JobStore
	overrides *TrustOverrideStore
}

func NewHandler(store *remediation.JobStore) *Handler {
	return &Handler{store: store, overrides: NewTrustOverrideStore()}
}

func (h *Handler) HandlePerformance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ComputePerformance(h.store.List()))
}

func (h *Handler) HandleTrustMatrix(w http.ResponseWriter, r *http.Request) {
	jobs := h.store.List()
	raw := computeTrustMatrixRaw(jobs)
	writeJSON(w, http.StatusOK, ApplyTrustOverrides(raw, h.overrides.List()))
}

func (h *Handler) HandleTrustOverrides(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, TrustOverridesResponse{
		GeneratedAt: time.Now().UTC(),
		Overrides:   h.overrides.List(),
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
	jobs := h.store.List()
	raw := computeTrustMatrixRaw(jobs)
	merged := ApplyTrustOverrides(raw, h.overrides.List())
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
	h.overrides.Put(TrustOverride{
		SkillID:   skillID,
		Level:     level,
		Reason:    reason,
		AppliedBy: appliedBy,
		AppliedAt: time.Now().UTC(),
	})
	final := ApplyTrustOverrides(raw, h.overrides.List())
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
	jobs := h.store.List()
	perf := ComputePerformance(jobs)
	rawTrust := computeTrustMatrixRaw(jobs)
	trust := ApplyTrustOverrides(rawTrust, h.overrides.List())
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
