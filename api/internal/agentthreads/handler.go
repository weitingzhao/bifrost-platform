package agentthreads

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
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
	if !h.collectorHost(w, r, b.Host) {
		return
	}
	p := actuation.PrincipalFromContext(r.Context())
	if host, ok := actuation.CollectorHost(p); ok {
		b.boundHost = host
	}
	if actuation.IsCollector(p) && (b.titlePresent || b.Title != "") {
		// Titles stay on the reporter's path (R5:5.5-12 C10): a collector's
		// beat stores none, so its list never carries one.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a collector's heartbeat carries no title"})
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
		case errors.Is(err, ErrOtherHost):
			// Another host's thread under the same id (R5:5.5-12 §1.4): not
			// changed, whatever key the beat holds. The audit names the token,
			// not the other host.
			if h.audit != nil {
				h.audit.Record(r, "agent_thread.host_rejected", p.Name, "denied", "a collector updates only its own host's threads")
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "a collector token reports only for its own host's threads"})
			return
		}
		h.unavailable(w, r, err)
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
	if !h.collectorHost(w, r, b.Host) {
		return
	}
	if err := h.rec.RecordHost(b); err != nil {
		if errors.Is(err, ErrHostFull) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": ErrHostFull.Error()})
			return
		}
		h.unavailable(w, r, err)
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

// collectorHost holds a collector token to the host its name binds it to
// (R5:5.5-12 §1.4): a beat naming another host is refused 403 and audited,
// as a key not held is (HandleBeat). Any other token passes: it reports for
// whichever host it names, as before.
func (h *Handler) collectorHost(w http.ResponseWriter, r *http.Request, named string) bool {
	p := actuation.PrincipalFromContext(r.Context())
	if !actuation.IsCollector(p) {
		return true
	}
	host, ok := actuation.CollectorHost(p)
	if ok && named == host {
		return true
	}
	if h.audit != nil {
		h.audit.Record(r, "agent_thread.host_rejected", p.Name, "denied", "a collector reports only for its own host")
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "a collector token reports only for its own host"})
	return false
}

// unavailable answers a failure of the shared state 503. A collector gets
// a fixed answer: the error of a store every host writes may carry another
// host's stored values (a decode error names them), so it is logged only.
func (h *Handler) unavailable(w http.ResponseWriter, r *http.Request, err error) {
	if actuation.IsCollector(actuation.PrincipalFromContext(r.Context())) {
		slog.Warn("agent_threads", "collector", true, "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the thread state is unavailable"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
}

// collectorList is what a collector reads of GET /api/v1/agent/threads
// (R5:5.5-12 §1.3): its own host's threads, without titles, and its own
// host's row; no other host and no count of refusals across hosts.
type collectorList struct {
	GeneratedAt          time.Time  `json:"generated_at"`
	SilentAfterSeconds   int64      `json:"silent_after_seconds"`
	ToolGraceSeconds     int64      `json:"tool_grace_seconds"`
	HostLostAfterSeconds int64      `json:"host_lost_after_seconds"`
	Threads              []View     `json:"threads"`
	Hosts                []HostView `json:"hosts"`
}

// ownHost is the part of st a collector bound to host reads: that host's
// threads, each without a title (the title store is global and keyed by
// session alone, C10), and that host's row. Statuses are reckoned from
// this part alone, so no other host's rows bear on the answer.
func (c Config) ownHost(st State, host string) (Config, State) {
	own := State{Threads: map[string]Thread{}, Hosts: map[string]Host{}}
	for k, t := range st.Threads {
		if t.Host == host {
			t.Title = ""
			own.Threads[k] = t
		}
	}
	for k, hb := range st.Hosts {
		if hb.Host == host {
			own.Hosts[k] = hb
		}
	}
	expected := c.ExpectedHosts
	c.ExpectedHosts = nil
	if slices.Contains(expected, host) {
		c.ExpectedHosts = []string{host}
	}
	return c, own
}

// HandleList is GET /api/v1/agent/threads: every retained thread with its
// status, newest first. A collector reads its own host only (ownHost).
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	st, err := h.rec.State()
	if err != nil {
		h.unavailable(w, r, err)
		return
	}
	if p := actuation.PrincipalFromContext(r.Context()); actuation.IsCollector(p) {
		host, ok := actuation.CollectorHost(p)
		if !ok {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "a collector token is bound to a host"})
			return
		}
		cfg, own := h.cfg.ownHost(st, host)
		now := h.now().UTC()
		writeJSON(w, http.StatusOK, collectorList{
			GeneratedAt:          now,
			SilentAfterSeconds:   secs(cfg.SilentAfter),
			ToolGraceSeconds:     secs(cfg.ToolGrace),
			HostLostAfterSeconds: secs(cfg.HostLostAfter),
			Threads:              cfg.Views(own, now),
			Hosts:                cfg.HostViews(own, now),
		})
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
