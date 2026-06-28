package cluster

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog) *Handler {
	entry := cfg.DefaultCluster()
	return &Handler{svc: NewService(entry), audit: audit}
}

func (h *Handler) HandleSummary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Summary(r.Context()))
}

func (h *Handler) HandleNodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Nodes(r.Context()))
}

func (h *Handler) HandlePlacement(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Placement(r.Context()))
}

func (h *Handler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 8)
	writeJSON(w, http.StatusOK, h.svc.Metrics(r.Context(), limit))
}

func (h *Handler) HandleServiceReadiness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.ServiceReadiness(r.Context()))
}

func (h *Handler) HandlePostgresStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.PostgresStatus(r.Context()))
}

func (h *Handler) HandleRedisStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.RedisStatus(r.Context()))
}

func (h *Handler) HandleGovernance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Governance(r.Context()))
}

func (h *Handler) HandleObservability(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Observability(r.Context()))
}

func (h *Handler) HandleNamespaces(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("watch")
	writeJSON(w, http.StatusOK, h.svc.Namespaces(r.Context(), filter))
}

func (h *Handler) HandleWorkloads(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	writeJSON(w, http.StatusOK, h.svc.Workloads(r.Context(), ns))
}

func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	writeJSON(w, http.StatusOK, h.svc.Events(r.Context(), ns, limit))
}

func (h *Handler) HandleSyncKubeconfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	writeJSON(w, http.StatusOK, h.svc.SyncKubeconfig())
}

func (h *Handler) HandleEnsureKubeconfigSecret(w http.ResponseWriter, r *http.Request) {
	var req EnsureKubeconfigSecretRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
	}
	resp, err := h.svc.EnsureKubeconfigSecret(r.Context(), req)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	if err != nil {
		resp.OK = false
		if resp.Message == "" {
			resp.Message = err.Error()
		}
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleEnsureMetricsServer(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.EnsureMetricsServer()
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandleEnsureBifrost(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.EnsureBifrostNamespaces(r.Context())
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandleRolloutRestart(w http.ResponseWriter, r *http.Request) {
	var req RolloutRestartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	resp, err := h.svc.RolloutRestart(r.Context(), req)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandleScale(w http.ResponseWriter, r *http.Request) {
	var req ScaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	resp, err := h.svc.Scale(r.Context(), req)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandleDeletePod(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	resp, err := h.svc.DeletePod(r.Context(), namespace, name)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandlePodLogs(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	tailLines := int64(parseLimit(r.URL.Query().Get("tailLines"), 200))
	container := r.URL.Query().Get("container")
	resp, err := h.svc.PodLogs(r.Context(), namespace, name, tailLines, container)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleNodePower(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	resp, err := h.svc.NodePower(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleWakeNode(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	resp, err := h.svc.WakeNode(r.Context(), name)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandlePowerOffNode(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	resp, err := h.svc.PowerOffNode(r.Context(), name)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandleCordonNode(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	resp, err := h.svc.CordonNode(r.Context(), name)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandleUncordonNode(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	resp, err := h.svc.UncordonNode(r.Context(), name)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandleDrainNode(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req DrainNodeRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
	}
	resp, err := h.svc.DrainNode(r.Context(), name, req)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) HandleJoinProfiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.JoinProfiles())
}

func (h *Handler) HandleJoinNode(w http.ResponseWriter, r *http.Request) {
	var req JoinNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	resp, err := h.svc.JoinNode(r.Context(), req)
	h.recordAudit(r, resp.Action, resp.Target, auditStatus(err), resp.Message)
	writeActuationResponse(w, resp, err)
}

func (h *Handler) recordAudit(r *http.Request, action, target, status, detail string) {
	if h.audit != nil {
		h.audit.Record(r, action, target, status, detail)
	}
}

func writeActuationResponse(w http.ResponseWriter, resp ActuationResponse, err error) {
	if err != nil {
		resp.OK = false
		if resp.Message == "" {
			resp.Message = err.Error()
		}
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func auditStatus(err error) string {
	if err != nil {
		return "failed"
	}
	return "ok"
}

func parseLimit(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultVal
	}
	if n > 200 {
		return 200
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
