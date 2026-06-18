package delivery

import (
	"encoding/json"
	"net/http"
	"strings"

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

func (h *Handler) HandlePipelines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Pipelines(r.Context()))
}

func (h *Handler) HandlePipelineRuns(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline name required"})
		return
	}
	writeJSON(w, http.StatusOK, h.svc.PipelineRuns(r.Context(), name))
}

func (h *Handler) HandlePipelinePreflight(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline name required"})
		return
	}
	writeJSON(w, http.StatusOK, h.svc.PipelinePreflight(r.Context(), name))
}

func (h *Handler) HandleStartPipelineRun(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline name required"})
		return
	}
	resp, run, err := h.svc.StartPipelineRun(r.Context(), name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		h.audit.Record(r, resp.Action, resp.Target, status, resp.Message)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"action":  resp.Action,
			"target":  resp.Target,
			"message": resp.Message,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      resp.OK,
		"action":  resp.Action,
		"target":  resp.Target,
		"message": resp.Message,
		"run":     run,
	})
}

func (h *Handler) HandleStgSmoke(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.StgSmoke(r.Context()))
}

func (h *Handler) HandleRunLogs(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run id required"})
		return
	}
	ns := strings.TrimSpace(r.URL.Query().Get("ns"))
	logs, err := h.svc.RunLogs(r.Context(), ns, id)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (h *Handler) HandleDeletePipelineRun(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run id required"})
		return
	}
	ns := strings.TrimSpace(r.URL.Query().Get("ns"))
	resp, err := h.svc.DeletePipelineRun(r.Context(), ns, id)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		h.audit.Record(r, resp.Action, resp.Target, status, resp.Message)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"action":  resp.Action,
			"target":  resp.Target,
			"message": resp.Message,
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
