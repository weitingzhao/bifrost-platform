package vision

import (
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog) *Handler {
	return &Handler{svc: NewService(cfg), audit: audit}
}

func (h *Handler) HandleGetV1Gate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.V1Gate(r.Context()))
}

type v1SignoffRequest struct {
	Notes string `json:"notes"`
}

func (h *Handler) HandleRunV1Gate(w http.ResponseWriter, r *http.Request) {
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunV1Gate(r.Context(), principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok": false, "action": "vision.v1-gate", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSignV1(w http.ResponseWriter, r *http.Request) {
	var req v1SignoffRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.SignV1(r.Context(), req.Notes, principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.v1-signoff", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetS3Gate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.S3Gate(r.Context()))
}

func (h *Handler) HandleRunS3Gate(w http.ResponseWriter, r *http.Request) {
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunS3Gate(r.Context(), principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok": false, "action": "vision.s3-gate", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSignS3(w http.ResponseWriter, r *http.Request) {
	var req v1SignoffRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.SignS3(r.Context(), req.Notes, principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.s3-signoff", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetV2Gate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.V2Gate(r.Context()))
}

func (h *Handler) HandleRunV2Gate(w http.ResponseWriter, r *http.Request) {
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunV2Gate(r.Context(), principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok": false, "action": "vision.v2-gate", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSignV2(w http.ResponseWriter, r *http.Request) {
	var req v1SignoffRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.SignV2(r.Context(), req.Notes, principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.v2-signoff", "message": err.Error(),
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
