package approvals

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
)

// SessionHeader is the caller identity stored as requester.
const SessionHeader = "X-Bifrost-Session"

// Mount registers the catalog and approval routes on an /api/v1 router.
func Mount(r chi.Router, auth *actuation.AuthService, svc *Service) {
	r.Get("/actions", actions.HandleList)
	r.Group(func(r chi.Router) {
		r.Use(auth.Require(actuation.RoleViewer))
		r.Get("/approvals", svc.HandleList)
		r.Get("/approvals/{id}", svc.HandleGet)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.Require(actuation.RoleOperator))
		r.Post("/approvals", svc.HandleCreate)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.Require(actuation.RoleAdmin))
		r.Post("/approvals/{id}/approve", svc.HandleApprove)
		r.Post("/approvals/{id}/reject", svc.HandleReject)
	})
}

func (s *Service) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string         `json:"action"`
		Params   map[string]any `json:"params"`
		Reason   string         `json:"reason"`
		Rollback string         `json:"rollback"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	res := s.create(r.Context(), requester(r), body.Action, body.Reason, body.Rollback, body.Params)
	if res.Status == http.StatusCreated {
		if s.audit != nil {
			s.audit.Record(r, "approval.create", res.Approval.ID, StatusPending,
				fmt.Sprintf("action=%s tier=%s requester=%s", res.Approval.Action, res.Approval.Tier, res.Approval.Requester))
		}
		id := res.Approval.ID
		tier := string(res.Approval.Tier)
		// A down relay must not fail create. NotifyCreated logs its own error.
		_ = approvalnotify.NotifyCreated(r.Context(), approvalnotify.Created{
			ID:        id,
			Action:    body.Action,
			Tier:      tier,
			Requester: r.Header.Get("X-Bifrost-Session"),
		})
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":          res.Approval.ID,
			"action":      res.Approval.Action,
			"tier":        res.Approval.Tier,
			"params_hash": res.Approval.ParamsHash,
			"status":      res.Approval.Status,
			"requester":   res.Approval.Requester,
			"expires_at":  res.Approval.ExpiresAt,
		})
		return
	}
	writeJSON(w, res.Status, res.Body)
}

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "pending"
	}
	list, code, body := s.list(status)
	if code != http.StatusOK {
		writeJSON(w, code, body)
		return
	}
	if list == nil {
		list = []Approval{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": list})
}

func (s *Service) HandleGet(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.get(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Service) HandleApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	id := chi.URLParam(r, "id")
	out := s.approve(r.Context(), id, body.Channel)
	if out.Status == http.StatusOK && s.audit != nil {
		s.audit.Record(r, "approval.approve", id, "approved", "channel="+strings.TrimSpace(body.Channel))
		status, _ := out.Body["status"].(string)
		detail := status
		if errText, _ := out.Body["error"].(string); errText != "" {
			detail = errText
		}
		s.audit.Record(r, "approval.execute", id, status, detail)
	}
	writeJSON(w, out.Status, out.Body)
}

func (s *Service) HandleReject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	id := chi.URLParam(r, "id")
	out := s.reject(id, body.Reason)
	if out.Status == http.StatusOK && s.audit != nil {
		s.audit.Record(r, "approval.reject", id, StatusRejected, strings.TrimSpace(body.Reason))
	}
	writeJSON(w, out.Status, out.Body)
}

func requester(r *http.Request) string {
	if s := strings.TrimSpace(r.Header.Get(SessionHeader)); s != "" {
		return s
	}
	return actuation.PrincipalFromContext(r.Context()).Name
}

func decode(r *http.Request, dest any) error {
	if r.Body == nil {
		return fmt.Errorf("empty body")
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dest); err != nil {
		if err == io.EOF {
			return fmt.Errorf("empty body")
		}
		return fmt.Errorf("invalid json body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
