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

func (h *Handler) recordAudit(r *http.Request, action, target, status, detail string) {
	if h.audit != nil {
		h.audit.Record(r, action, target, status, detail)
	}
}

func writeActuationResponse(w http.ResponseWriter, resp ActuationResponse, err error) {
	if err != nil {
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
