package workactions

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Handler is the HTTP surface. C and D calls are stopped by the action guard
// before they reach these methods.
type Handler struct {
	Svc *Service
}

func (h *Handler) HandlePlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo   string `json:"repo"`
		Path   string `json:"path"`
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := h.Svc.Plan(r.Context(), body.Repo, body.Path, body.Commit)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"plan_id": id,
		"logs":    "/api/v1/delivery/runs/" + id + "/logs",
	})
}

func (h *Handler) HandleGetPlan(w http.ResponseWriter, r *http.Request) {
	summary, err := h.Svc.Summarize(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ready":      summary.Ready,
		"policy":     summary.Policy,
		"namespaces": summary.Namespaces,
		"daemon":     summary.Daemon,
		"objects":    summary.Objects,
		"repo":       summary.Repo,
		"path":       summary.Path,
		"commit":     summary.Commit,
		"logs":       summary.Logs,
	})
}

func (h *Handler) HandleApply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := h.Svc.Apply(r.Context(), strings.TrimSpace(body.PlanID))
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (h *Handler) HandleCreateJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Namespace string `json:"namespace"`
		CronJob   string `json:"cronjob"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := h.Svc.CreateJobFromCronJob(r.Context(), body.Namespace, body.CronJob, requester(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (h *Handler) HandleDeleteFinished(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Namespace     string   `json:"namespace"`
		Names         []string `json:"names"`
		LabelSelector string   `json:"label_selector"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := h.Svc.DeleteFinished(r.Context(), body.Namespace, body.Names, body.LabelSelector)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) HandleProbe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Namespace      string   `json:"namespace"`
		Image          string   `json:"image"`
		Command        []string `json:"command"`
		Args           []string `json:"args"`
		EnvFrom        string   `json:"env_from"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := h.Svc.Probe(r.Context(), body.Namespace, body.Image, body.Command, body.Args, body.EnvFrom, body.TimeoutSeconds, requester(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func requester(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Bifrost-Session")); v != "" {
		return v
	}
	return "platform"
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
