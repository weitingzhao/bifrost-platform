package agentthreads

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Auditor records a rejected thread-key write. *actuation.AuditLog satisfies it.
type Auditor interface {
	Record(r *http.Request, action, target, status, detail string)
}

// Handler serves the thread and host heartbeats and the list.
type Handler struct {
	rec    *Recorder
	cfg    Config
	titles TitlesFunc
	audit  Auditor
	now    func() time.Time
}

func NewHandler(rec *Recorder, cfg Config, titles TitlesFunc, audit Auditor) *Handler {
	return &Handler{rec: rec, cfg: cfg, titles: titles, audit: audit, now: time.Now}
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
	dec, err := h.rec.Admit(b)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownKey):
			if h.audit != nil {
				h.audit.Record(r, "agent_thread.key_rejected", Key(b.Vendor, b.Thread), "denied", "unknown key")
			}
			writeJSON(w, http.StatusConflict, map[string]string{"error": "unknown key"})
			return
		case errors.Is(err, ErrOlderTurn):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "older turn refused"})
			return
		case errors.Is(err, ErrThreadFull):
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": ErrThreadFull.Error()})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	body := map[string]any{"ok": true}
	if dec.Ignored {
		body["ignored"] = true
	}
	if dec.Key != "" {
		body["thread_key"] = dec.Key
	}
	writeJSON(w, http.StatusAccepted, body)
}

// HandleHost is POST /api/v1/agent/hosts/heartbeat (reporter): one machine
// reports its hook wiring and whether the reporter token is readable.
func (h *Handler) HandleHost(w http.ResponseWriter, r *http.Request) {
	var b HostBeat
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body: " + err.Error()})
		return
	}
	if err := b.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.rec.RecordHost(b); err != nil {
		if errors.Is(err, ErrHostFull) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": ErrHostFull.Error()})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

type listResponse struct {
	GeneratedAt          time.Time  `json:"generated_at"`
	SilentAfterSeconds   int64      `json:"silent_after_seconds"`
	ToolGraceSeconds     int64      `json:"tool_grace_seconds"`
	HostLostAfterSeconds int64      `json:"host_lost_after_seconds"`
	Threads              []View     `json:"threads"`
	Hosts                []HostView `json:"hosts"`
	// ThreadsRefused and LastThreadRefusal are the capacity gap. The refused
	// threads themselves are not in Threads.
	ThreadsRefused    int       `json:"threads_refused"`
	LastThreadRefusal time.Time `json:"last_thread_refusal,omitempty"`
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
		GeneratedAt:          now,
		SilentAfterSeconds:   secs(h.cfg.SilentAfter),
		ToolGraceSeconds:     secs(h.cfg.ToolGrace),
		HostLostAfterSeconds: secs(h.cfg.HostLostAfter),
		Threads:              h.cfg.Views(st, now),
		Hosts:                h.cfg.HostViews(st, now),
		ThreadsRefused:       st.ThreadsRefused,
		LastThreadRefusal:    st.LastThreadRefusal,
	})
}
