package agentthreads

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Handler serves the two routes.
type Handler struct {
	rec    *Recorder
	cfg    Config
	titles TitlesFunc
	now    func() time.Time
}

func NewHandler(rec *Recorder, cfg Config, titles TitlesFunc) *Handler {
	return &Handler{rec: rec, cfg: cfg, titles: titles, now: time.Now}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// HandleBeat is POST /api/v1/agent/threads/heartbeat (reporter): one session
// reports one of its own events.
func (h *Handler) HandleBeat(w http.ResponseWriter, r *http.Request) {
	var b Beat
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body: " + err.Error()})
		return
	}
	if err := b.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.rec.Record(b)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

type listResponse struct {
	GeneratedAt        time.Time `json:"generated_at"`
	SilentAfterSeconds int64     `json:"silent_after_seconds"`
	ToolGraceSeconds   int64     `json:"tool_grace_seconds"`
	Threads            []View    `json:"threads"`
}

// HandleList is GET /api/v1/agent/threads: every retained thread with its
// status, newest first.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	st, err := h.rec.State()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	if h.titles != nil {
		if titles, err := h.titles(r.Context()); err != nil {
			slog.Warn("agent_threads_list", "titles", err)
		} else {
			for k, t := range st.Threads {
				if title, found := titles[t.Thread]; found {
					t.Title = title
					st.Threads[k] = t
				}
			}
		}
	}
	now := h.now().UTC()
	writeJSON(w, http.StatusOK, listResponse{
		GeneratedAt:        now,
		SilentAfterSeconds: secs(h.cfg.SilentAfter),
		ToolGraceSeconds:   secs(h.cfg.ToolGrace),
		Threads:            h.cfg.Views(st, now),
	})
}
