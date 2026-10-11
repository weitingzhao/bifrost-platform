package questions

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

const sessionHeader = "X-Bifrost-Session"

// Mount registers the question routes on an /api/v1 router.
// {id} is q_… or a number (Q12, 12).
func Mount(r chi.Router, auth *actuation.AuthService, svc *Service) {
	if svc == nil {
		return
	}
	r.Group(func(r chi.Router) {
		r.Use(auth.Require(actuation.RoleViewer))
		r.Get("/questions", svc.HandleList)
		r.Get("/questions/{id}", svc.HandleGet)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.Require(actuation.RoleReporter))
		r.Post("/questions", svc.HandleCreate)
		r.Post("/questions/{id}/withdraw", svc.HandleWithdraw)
		r.Post("/questions/{id}/ack", svc.HandleAck)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.Require(actuation.RoleAdmin))
		r.Post("/questions/{id}/answer", svc.HandleAnswer)
	})
}

func (s *Service) HandleCreate(w http.ResponseWriter, r *http.Request) {
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	session, principal, role, ok := callerFrom(w, r)
	if !ok {
		return
	}
	writeResult(w, s.Create(r.Context(), session, principal, role, raw))
}

func (s *Service) HandleGet(w http.ResponseWriter, r *http.Request) {
	session, principal, role, ok := callerFrom(w, r)
	if !ok {
		return
	}
	writeResult(w, s.Get(r.Context(), session, principal, role, chi.URLParam(r, "id")))
}

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	session, principal, role, ok := callerFrom(w, r)
	if !ok {
		return
	}
	mine := r.URL.Query().Get("mine") == "1"
	writeResult(w, s.List(r.Context(), session, principal, role, r.URL.Query().Get("status"), mine))
}

func (s *Service) HandleWithdraw(w http.ResponseWriter, r *http.Request) {
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	session, principal, role, ok := callerFrom(w, r)
	if !ok {
		return
	}
	writeResult(w, s.Withdraw(r.Context(), session, principal, role, chi.URLParam(r, "id"), raw))
}

func (s *Service) HandleAck(w http.ResponseWriter, r *http.Request) {
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	session, principal, role, ok := callerFrom(w, r)
	if !ok {
		return
	}
	writeResult(w, s.Ack(r.Context(), session, principal, role, chi.URLParam(r, "id"), raw))
}

func (s *Service) HandleAnswer(w http.ResponseWriter, r *http.Request) {
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	session, principal, role, ok := callerFrom(w, r)
	if !ok {
		return
	}
	writeResult(w, s.Answer(r.Context(), session, principal, role, chi.URLParam(r, "id"), raw))
}

func callerFrom(w http.ResponseWriter, r *http.Request) (session, principal, role string, ok bool) {
	session = r.Header.Get(sessionHeader)
	if err := ValidSession(session); err != nil {
		writeResult(w, fail(http.StatusBadRequest, err.Error(), nil))
		return "", "", "", false
	}
	p := actuation.PrincipalFromContext(r.Context())
	return session, p.Name, string(p.Role), true
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		return nil, true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeResult(w, fail(http.StatusBadRequest, "invalid json body", nil))
		return nil, false
	}
	if len(raw) > maxBodyBytes {
		writeResult(w, fail(http.StatusRequestEntityTooLarge, "request body exceeds 16 KiB", nil))
		return nil, false
	}
	return raw, true
}

func writeResult(w http.ResponseWriter, res Result) {
	writeJSON(w, res.Code, res.Body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
