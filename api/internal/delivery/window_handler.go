package delivery

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (h *Handler) HandleGetReleaseWindow(w http.ResponseWriter, r *http.Request) {
	cs, _, err := h.svc.cluster.KubernetesClient()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	rec, open, err := getReleaseWindow(windowContext(r.Context(), h.svc.windowRecorder), cs, h.svc.PipelinesNamespace(), time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().UTC()
	out := map[string]any{"open": open}
	cm, gerr := cs.CoreV1().ConfigMaps(h.svc.PipelinesNamespace()).Get(r.Context(), releaseWindowConfigMap, metav1.GetOptions{})
	if gerr == nil {
		// The window's version: it changes when the record does (R10), while
		// the rest of this answer also moves with the clock.
		out["version"] = cm.ResourceVersion
	}
	if open {
		out["window"] = rec
		out["holder"] = viewHolder(rec, now)
	} else if gerr == nil {
		rec, _ = parseWindowCM(cm) // a free window still shows who waits
	}
	out["queue"] = viewQueue(rec.Queue, now)
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) HandlePutReleaseWindow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		What       string `json:"what"`
		Who        string `json:"who"`
		Reason     string `json:"reason"`
		TTLMinutes int    `json:"ttl_minutes"`
		Env        string `json:"env"`
		// Queue (window_queue.go): wait behind a live holder; 202 instead of 409.
		Queue        bool   `json:"queue"`
		Thread       string `json:"thread"`
		Resume       string `json:"resume"`
		QueueMinutes int    `json:"queue_minutes"`
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
	rec, err := putReleaseWindow(windowContext(r.Context(), h.svc.windowRecorder), cs, h.svc.PipelinesNamespace(), windowInput{
		What: body.What, Who: body.Who, Reason: body.Reason, Env: body.Env, TTLMinutes: body.TTLMinutes,
		Queue: body.Queue, Thread: body.Thread, Resume: body.Resume, QueueMinutes: body.QueueMinutes,
	}, time.Now().UTC())
	if err != nil {
		if queued, ok := err.(*WindowQueued); ok {
			if h.audit != nil {
				h.audit.Record(r, "delivery.release_window.queue", body.Who, "ok", body.What)
			}
			now := time.Now().UTC()
			writeJSON(w, http.StatusAccepted, map[string]any{"open": false, "queued": true, "position": queued.Position,
				"holder": viewHolder(queued.Holder, now), "queue": viewQueue(queued.Queue, now)})
			return
		}
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
	err = deleteReleaseWindow(windowContext(r.Context(), h.svc.windowRecorder), cs, h.svc.PipelinesNamespace(), body.Who, force, time.Now().UTC())
	if err != nil {
		if _, ok := err.(*WindowChanged); ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
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
