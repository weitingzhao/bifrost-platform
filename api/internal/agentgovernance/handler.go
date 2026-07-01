package agentgovernance

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store *remediation.JobStore
}

func NewHandler(store *remediation.JobStore) *Handler {
	return &Handler{store: store}
}

func (h *Handler) HandlePerformance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ComputePerformance(h.store.List()))
}

func (h *Handler) HandleTrustMatrix(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ComputeTrustMatrix(h.store.List()))
}

func (h *Handler) HandleCapabilityMap(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ComputeCapabilityMap())
}

func (h *Handler) HandleSnapshot(w http.ResponseWriter, r *http.Request) {
	jobs := h.store.List()
	perf := ComputePerformance(jobs)
	trust := ComputeTrustMatrix(jobs)
	capMap := ComputeCapabilityMap()
	brief := ComputeBriefing(jobs, trust)
	hermes := hermesConfigured()
	sources := []string{"remediation_jobs", "agent_task_catalog", "mcp_catalog"}
	if hermes {
		sources = append(sources, "nous_hermes_optional")
	}
	note := "Flight Director KPIs sourced from remediation runner JobStore. Hermes/GPU LLM path bypassed — trust matrix uses catalog defaults + job history."
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
