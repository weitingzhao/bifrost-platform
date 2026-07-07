package vision

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/devagent"
)

type Handler struct {
	svc      *Service
	audit    *actuation.AuditLog
	programs *devagent.Handler
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog) *Handler {
	return &Handler{svc: NewService(cfg), audit: audit}
}

func (h *Handler) BindPrograms(da *devagent.Handler) {
	h.programs = da
}

func (h *Handler) recordVisionPhaseSignoff(phaseID, signedBy, notes string, at time.Time) error {
	if h.programs == nil {
		return nil
	}
	err := h.programs.RecordPhaseSignoff(
		"vision",
		phaseID,
		signedBy,
		at.UTC().Format(time.RFC3339),
		notes,
	)
	if err != nil && strings.Contains(err.Error(), "already signed off") {
		return nil
	}
	return err
}

func (h *Handler) completeVisionSign(w http.ResponseWriter, r *http.Request, resp SignoffResponse, phaseID, notes, signedBy string) {
	if syncErr := h.recordVisionPhaseSignoff(phaseID, signedBy, notes, resp.GeneratedAt); syncErr != nil {
		if h.audit != nil {
			h.audit.Record(r, resp.Action, resp.Target, "failed", syncErr.Error())
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok": false, "action": resp.Action, "message": syncErr.Error(),
		})
		return
	}
	if h.audit != nil {
		h.audit.Record(r, resp.Action, resp.Target, "ok", resp.Message)
	}
	writeJSON(w, http.StatusOK, resp)
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
	if err != nil {
		if h.audit != nil {
			h.audit.Record(r, resp.Action, resp.Target, "failed", err.Error())
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.v1-signoff", "message": err.Error(),
		})
		return
	}
	h.completeVisionSign(w, r, resp, "V1", req.Notes, principal.Name)
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
	if err != nil {
		if h.audit != nil {
			h.audit.Record(r, resp.Action, resp.Target, "failed", err.Error())
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.s3-signoff", "message": err.Error(),
		})
		return
	}
	h.completeVisionSign(w, r, resp, "S3", req.Notes, principal.Name)
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
	if err != nil {
		if h.audit != nil {
			h.audit.Record(r, resp.Action, resp.Target, "failed", err.Error())
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.v2-signoff", "message": err.Error(),
		})
		return
	}
	h.completeVisionSign(w, r, resp, "V2", req.Notes, principal.Name)
}

func (h *Handler) HandleGetV3Gate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.V3Gate(r.Context()))
}

func (h *Handler) HandleRunV3Gate(w http.ResponseWriter, r *http.Request) {
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunV3Gate(r.Context(), principal.Name)
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
			"ok": false, "action": "vision.v3-gate", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSignV3(w http.ResponseWriter, r *http.Request) {
	var req v1SignoffRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.SignV3(r.Context(), req.Notes, principal.Name)
	if err != nil {
		if h.audit != nil {
			h.audit.Record(r, resp.Action, resp.Target, "failed", err.Error())
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.v3-signoff", "message": err.Error(),
		})
		return
	}
	h.completeVisionSign(w, r, resp, "V3", req.Notes, principal.Name)
}

func (h *Handler) HandleGetV4Gate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.V4Gate(r.Context()))
}

func (h *Handler) HandleRunV4Gate(w http.ResponseWriter, r *http.Request) {
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunV4Gate(r.Context(), principal.Name)
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
			"ok": false, "action": "vision.v4-gate", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSignV4(w http.ResponseWriter, r *http.Request) {
	var req v1SignoffRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.SignV4(r.Context(), req.Notes, principal.Name)
	if err != nil {
		if h.audit != nil {
			h.audit.Record(r, resp.Action, resp.Target, "failed", err.Error())
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.v4-signoff", "message": err.Error(),
		})
		return
	}
	h.completeVisionSign(w, r, resp, "V4", req.Notes, principal.Name)
}

func (h *Handler) HandleGetV5Gate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.V5Gate(r.Context()))
}

func (h *Handler) HandleRunV5Gate(w http.ResponseWriter, r *http.Request) {
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunV5Gate(r.Context(), principal.Name)
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
			"ok": false, "action": "vision.v5-gate", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSignV5(w http.ResponseWriter, r *http.Request) {
	var req v1SignoffRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.SignV5(r.Context(), req.Notes, principal.Name)
	if err != nil {
		if h.audit != nil {
			h.audit.Record(r, resp.Action, resp.Target, "failed", err.Error())
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "vision.v5-signoff", "message": err.Error(),
		})
		return
	}
	h.completeVisionSign(w, r, resp, "V5", req.Notes, principal.Name)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
