package delivery

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) HandleGetReleaseWindow(w http.ResponseWriter, r *http.Request) {
	cs, _, err := h.svc.cluster.KubernetesClient()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	rec, open, err := getReleaseWindow(r.Context(), cs, h.svc.PipelinesNamespace(), time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if !open {
		writeJSON(w, http.StatusOK, map[string]any{"open": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"open": true, "window": rec})
}

func (h *Handler) HandlePutReleaseWindow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		What       string `json:"what"`
		Who        string `json:"who"`
		Reason     string `json:"reason"`
		TTLMinutes int    `json:"ttl_minutes"`
		Env        string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	cs, _, err := h.svc.cluster.KubernetesClient()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	rec, err := putReleaseWindow(r.Context(), cs, h.svc.PipelinesNamespace(), windowInput{
		What: body.What, Who: body.Who, Reason: body.Reason, Env: body.Env, TTLMinutes: body.TTLMinutes,
	}, time.Now().UTC())
	if err != nil {
		if held, ok := err.(*WindowHeld); ok {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "release window held by someone else",
				"who":   held.Who,
				"what":  held.What,
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.Record(r, "delivery.release_window.hold", rec.Who, "ok", rec.What)
	}
	writeJSON(w, http.StatusOK, map[string]any{"open": true, "window": rec})
}

func (h *Handler) HandleDeleteReleaseWindow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Who string `json:"who"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
	}
	if strings.TrimSpace(body.Who) == "" {
		body.Who = strings.TrimSpace(r.URL.Query().Get("who"))
	}
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	cs, _, err := h.svc.cluster.KubernetesClient()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	err = deleteReleaseWindow(r.Context(), cs, h.svc.PipelinesNamespace(), body.Who, force, time.Now().UTC())
	if err != nil {
		if held, ok := err.(*WindowNotHolder); ok {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "only the holder can release the window",
				"who":   held.Who,
			})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.Record(r, "delivery.release_window.release", body.Who, "ok", "")
	}
	writeJSON(w, http.StatusOK, map[string]any{"open": false})
}

func (h *Handler) HandleSyncMirrors(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repos   []string          `json:"repos"`
		Commits map[string]string `json:"commits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	res, err := h.svc.SyncMirrors(r.Context(), body.Repos, body.Commits)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "cluster") || strings.Contains(err.Error(), "http") {
			status = http.StatusBadGateway
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.Record(r, "delivery.mirrors.sync", strings.Join(body.Repos, ","), "ok", "")
	}
	writeJSON(w, http.StatusOK, res)
}
