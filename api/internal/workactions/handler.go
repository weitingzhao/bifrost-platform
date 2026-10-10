package workactions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
)

// Handler is the HTTP surface. C and D calls are stopped by the action guard
// before they reach these methods.
type Handler struct {
	Svc *Service
	// Audit gets one record per call that reaches the service (TD-279). A
	// direct B-tier call has no approval row, so this is its only trace.
	Audit *actuation.AuditLog
}

// maxAuditDetail keeps a probe's command line from filling the record.
const maxAuditDetail = 400

func (h *Handler) record(r *http.Request, action, target string, err error, detail string) {
	if h.Audit == nil {
		return
	}
	status := "ok"
	// Redact the caller text before the length clip so a cut cannot leave a
	// secret that the marker no longer matches. The error and the requester
	// are appended after that clip; they are redacted too.
	detail = approvalnotify.Redact(detail)
	if len(detail) > maxAuditDetail {
		detail = detail[:maxAuditDetail]
	}
	if err != nil {
		status = "failed"
		detail = strings.TrimSpace(detail + " error=" + approvalnotify.Redact(err.Error()))
	}
	detail = strings.TrimSpace(detail + " requester=" + requester(r))
	h.Audit.Record(r, action, target, status, detail)
}

func resultText(out map[string]any, keys ...string) string {
	var parts []string
	for _, k := range keys {
		if v, ok := out[k]; ok && v != nil {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	return strings.Join(parts, " ")
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
	h.record(r, "plan_manifest", body.Repo+":"+body.Path+"@"+body.Commit, err, "plan_id="+id)
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
	h.record(r, "apply_manifest", strings.TrimSpace(body.PlanID), err, resultText(out, "apply_run", "tier"))
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
	h.record(r, "create_job_from_cronjob", body.Namespace+"/"+body.CronJob, err, resultText(out, "job"))
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
	target := body.Namespace + "/" + strings.Join(body.Names, ",")
	if body.LabelSelector != "" {
		target = body.Namespace + "/" + body.LabelSelector
	}
	h.record(r, "delete_finished_jobs", target, err, resultText(out, "jobs", "probe_pods"))
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
	h.record(r, "run_probe_pod", body.Namespace+"/"+body.Image, err,
		strings.TrimSpace(resultText(out, "job")+" env_from="+body.EnvFrom+" command="+strings.Join(append(append([]string{}, body.Command...), body.Args...), " ")))
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
