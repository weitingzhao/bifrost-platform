package agentgovernance

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store     *remediation.JobStore
	overrides TrustOverrideStore
}

func NewHandler(store *remediation.JobStore) *Handler {
	_ = ensureAgentTasks()
	return &Handler{store: store, overrides: NewYAMLTrustOverrideStore("config/trust-overrides.yaml")}
}

// UseTrustOverrideStore points this handler at the release file.
func (h *Handler) UseTrustOverrideStore(s TrustOverrideStore) { h.overrides = s }

// TrustOverrideLocation names where this instance keeps the overrides.
func (h *Handler) TrustOverrideLocation() string { return h.overrides.Location() }

// listOverrides returns an empty map and a store_error string when the file
// cannot be read. Callers still answer 200: Research reads the matrix daily.
func (h *Handler) listOverrides(r *http.Request) (map[string]TrustOverride, string) {
	o, err := h.overrides.List(r.Context())
	if err != nil {
		slog.Error("trust overrides unreadable", "store", h.overrides.Location(), "err", err)
		return map[string]TrustOverride{}, err.Error()
	}
	if o == nil {
		o = map[string]TrustOverride{}
	}
	return o, ""
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
	overrides, storeErr := h.listOverrides(r)
	jobs := h.store.List()
	raw := computeTrustMatrixRaw(jobs)
	resp := ApplyTrustOverrides(raw, overrides)
	resp.OverrideStore = h.overrides.Location()
	resp.StoreError = storeErr
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleTrustOverrides(w http.ResponseWriter, r *http.Request) {
	overrides, storeErr := h.listOverrides(r)
	writeJSON(w, http.StatusOK, TrustOverridesResponse{
		GeneratedAt: time.Now().UTC(),
		Overrides:   overrides,
		Store:       h.overrides.Location(),
		StoreError:  storeErr,
	})
}

func (h *Handler) HandleCapabilityMap(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ComputeCapabilityMap())
}

func (h *Handler) HandleSnapshot(w http.ResponseWriter, r *http.Request) {
	overrides, _ := h.listOverrides(r)
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
	note := "Flight Director KPIs sourced from remediation runner JobStore. Owner trust overrides come from config/trust-overrides.yaml. Hermes/GPU LLM path bypassed."
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
