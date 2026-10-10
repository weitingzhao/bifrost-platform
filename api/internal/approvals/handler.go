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
// {id} is an approval id or its number ("57", "#57").
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
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAny(actuation.RoleExecutor, actuation.RoleAdmin))
		r.Post("/approvals/claim", svc.HandleClaim)
		r.Post("/approvals/{id}/heartbeat", svc.HandleHeartbeat)
		r.Post("/approvals/{id}/result", svc.HandleResult)
	})
}

func (s *Service) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action          string         `json:"action"`
		Params          map[string]any `json:"params"`
		Reason          string         `json:"reason"`
		Rollback        string         `json:"rollback"`
		RequesterThread string         `json:"requester_thread"`
		WorkID          string         `json:"work_id"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	res := s.createWith(r.Context(), createInput{
		Requester: requester(r),
		Action:    body.Action,
		Reason:    body.Reason,
		Rollback:  body.Rollback,
		Params:    body.Params,
		Thread:    body.RequesterThread,
		WorkID:    body.WorkID,
	})
	if res.Status == http.StatusCreated {
		a := res.Approval
		if s.audit != nil {
			s.audit.Record(r, "approval.create", a.ID, StatusPending,
				fmt.Sprintf("number=%d action=%s tier=%s runner=%s requester=%s", a.Number, a.Action, a.Tier, a.Runner, a.Requester))
		}
		// A down relay must not fail create. NotifyCreated logs its own error.
		_ = approvalnotify.NotifyCreated(r.Context(), approvalnotify.Created{
			ID:        a.ID,
			Action:    body.Action,
			Tier:      a.Tier,
			Requester: r.Header.Get("X-Bifrost-Session"),
		})
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":          a.ID,
			"number":      a.Number,
			"action":      a.Action,
			"tier":        a.Tier,
			"params_hash": a.ParamsHash,
			"status":      a.Status,
			"requester":   a.Requester,
			"expires_at":  a.ExpiresAt,
			"env":         a.Env,
			"summary":     a.Summary,
			"runner":      a.Runner,
		})
		return
	}
	writeJSON(w, res.Status, res.Body)
}

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = StatusPending
	}
	list, code, body := s.list(status)
	if code != http.StatusOK {
		writeJSON(w, code, body)
		return
	}
	out := make([]Approval, 0, len(list))
	for _, a := range list {
		out = append(out, a.public())
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": out})
}

func (s *Service) HandleGet(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.get(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, rec.public())
}

func (s *Service) HandleApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel       string          `json:"channel"`
		ConfirmNumber json.RawMessage `json:"confirm_number"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	confirm, err := confirmNumber(body.ConfirmNumber)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ref := chi.URLParam(r, "id")
	out := s.approveWith(r.Context(), ref, approveInput{Channel: body.Channel, Confirm: confirm})
	if (out.Status == http.StatusOK || out.Status == http.StatusAccepted) && s.audit != nil {
		id, _ := out.Body["id"].(string)
		s.audit.Record(r, "approval.approve", id, StatusApproved, "channel="+strings.TrimSpace(body.Channel))
		status, _ := out.Body["status"].(string)
		switch status {
		case StatusExecuted, StatusFailed:
			detail := status
			if errText, _ := out.Body["error"].(string); errText != "" {
				detail = errText
			}
			s.audit.Record(r, "approval.execute", id, status, detail)
		case StatusApproved:
			runner, _ := out.Body["runner"].(string)
			detail := "runner=" + runner
			if refusal, _ := out.Body["last_refusal"].(string); refusal != "" {
				detail += " refusal=" + refusal
			}
			s.audit.Record(r, "approval.queue", id, StatusApproved, detail)
		}
	}
	writeJSON(w, out.Status, out.Body)
}

// confirmNumber accepts 57, "57" and "#57"; nil when absent.
func confirmNumber(raw json.RawMessage) (*int, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		s = text
	}
	n, ok := parseNumber(s)
	if !ok {
		return nil, fmt.Errorf("confirm_number must be the approval number")
	}
	return &n, nil
}

func (s *Service) HandleReject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out := s.reject(chi.URLParam(r, "id"), body.Reason)
	if out.Status == http.StatusOK && s.audit != nil {
		id, _ := out.Body["id"].(string)
		s.audit.Record(r, "approval.reject", id, StatusRejected, strings.TrimSpace(body.Reason))
	}
	writeJSON(w, out.Status, out.Body)
}

// HandleClaim leases the oldest approved record this token may run (or the
// one named by id). 204 when there is none after wait_seconds (at most 25).
func (s *Service) HandleClaim(w http.ResponseWriter, r *http.Request) {
	var body claimInput
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	role := actuation.PrincipalFromContext(r.Context()).Role
	out := s.claim(r.Context(), role, body)
	s.record(r, out.events)
	if out.Status == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, out.Status, out.Body)
}

func (s *Service) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LeaseID string `json:"lease_id"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out := s.heartbeat(chi.URLParam(r, "id"), body.LeaseID)
	writeJSON(w, out.Status, out.Body)
}

func (s *Service) HandleResult(w http.ResponseWriter, r *http.Request) {
	var body resultInput
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out := s.result(chi.URLParam(r, "id"), body)
	s.record(r, out.events)
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
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
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
