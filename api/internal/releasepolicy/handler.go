package releasepolicy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// HandleStatus serves GET /api/v1/release-policy.
func (e *Engine) HandleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, e.Status(r.Context()))
}

// Audit records one write; the server passes its audit log in.
type Audit func(r *http.Request, action, target, result, detail string)

// HandleInstall serves PUT /api/v1/release-policy {policy_yaml, policy_sig}.
func (e *Engine) HandleInstall(audit Audit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PolicyYAML string `json:"policy_yaml"`
			PolicySig  string `json:"policy_sig"`
		}
		if !decode(w, r, &body) {
			return
		}
		id := PolicyID(body.PolicyYAML)
		e.respond(w, r, audit, "release_policy.install", id, func(ctx context.Context) (Status, error) {
			return e.Install(ctx, body.PolicyYAML, body.PolicySig)
		})
	}
}

// HandleFreeze serves POST /api/v1/release-policy/freeze {who, reason}.
func (e *Engine) HandleFreeze(audit Audit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Who    string `json:"who"`
			Reason string `json:"reason"`
		}
		if !decode(w, r, &body) {
			return
		}
		e.respond(w, r, audit, "release_policy.freeze", body.Who+": "+body.Reason, func(ctx context.Context) (Status, error) {
			return e.Freeze(ctx, body.Who, body.Reason)
		})
	}
}

// HandleUnfreeze serves POST /api/v1/release-policy/unfreeze {text, sig}.
func (e *Engine) HandleUnfreeze(audit Audit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
			Sig  string `json:"sig"`
		}
		if !decode(w, r, &body) {
			return
		}
		e.respond(w, r, audit, "release_policy.unfreeze", body.Text, func(ctx context.Context) (Status, error) {
			return e.Unfreeze(ctx, body.Text, body.Sig)
		})
	}
}

func (e *Engine) respond(w http.ResponseWriter, r *http.Request, audit Audit, action, target string, do func(context.Context) (Status, error)) {
	st, err := do(r.Context())
	result, code := "ok", http.StatusOK
	var payload any = st
	if err != nil {
		result, code = "refused", http.StatusConflict
		if !errors.Is(err, ErrRefused) {
			result, code = "error", http.StatusBadGateway
		}
		payload = map[string]string{"error": err.Error()}
	}
	if audit != nil {
		detail := ""
		if err != nil {
			detail = err.Error()
		}
		audit(r, action, target, result, detail)
	}
	writeJSON(w, code, payload)
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
