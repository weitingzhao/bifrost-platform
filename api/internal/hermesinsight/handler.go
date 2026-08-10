package hermesinsight

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/weitingzhao/bifrost-platform/api/internal/hermesreadiness"
)

// readinessProber is hermesreadiness.Handler.Build — do not invent a second probe.
type readinessProber interface {
	Build(ctx context.Context) hermesreadiness.ReadinessResponse
}

// Handler serves GET /api/v1/hermes/insights and POST /api/v1/hermes/run-first-task.
type Handler struct {
	store     *Store
	readiness readinessProber
	now       func() time.Time
}

type HandlerOptions struct {
	StateDir  string
	Readiness readinessProber
	Now       func() time.Time
}

func NewHandler() (*Handler, error) {
	return NewHandlerWithOptions(HandlerOptions{})
}

func NewHandlerWithOptions(opts HandlerOptions) (*Handler, error) {
	store, err := NewStore(opts.StateDir)
	if err != nil {
		return nil, err
	}
	ready := opts.Readiness
	if ready == nil {
		ready = hermesreadiness.NewHandler()
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &Handler{store: store, readiness: ready, now: nowFn}, nil
}

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	items, total := h.store.List(limit)
	if items == nil {
		items = []HermesInsight{}
	}
	writeJSON(w, http.StatusOK, ListResponse{Items: items, Total: total})
}

// HandleRunFirstTask is L0 / D10: readiness-gated analysis only.
// V1 does not spawn a remote Hermes chat session — hermesgateway has no run/session
// trigger. When ready we persist a local insight and tell Chat UI / Gateway to run
// the canonical L0 prompt (hermes-mission-health-l0). No trading, no kubectl writes.
func (h *Handler) HandleRunFirstTask(w http.ResponseWriter, r *http.Request) {
	task := hermesreadiness.FirstTask()
	started := h.now().UTC()
	ready := h.readiness.Build(r.Context())
	finished := h.now().UTC()
	duration := finished.Sub(started).Milliseconds()
	if duration < 0 {
		duration = 0
	}

	insight := HermesInsight{
		ID:         "hi-" + uuid.NewString(),
		Time:       started.Format(time.RFC3339),
		Symbol:     "",
		Type:       TypeFirstTask,
		Source:     SourceFirstTask,
		DurationMS: duration,
	}

	if !ready.Ready {
		errMsg := blockersError(ready)
		insight.Verdict = VerdictBlocked
		insight.Summary = task.Title + " (prompt_id=" + task.ID + ") blocked: " + errMsg
		if err := h.store.Append(insight); err != nil {
			slog.Warn("hermesinsight persist", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, RunFirstTaskResponse{OK: false, Insight: insight, Error: errMsg})
		return
	}

	insight.Verdict = VerdictOK
	insight.Summary = task.Title + " (prompt_id=" + task.ID +
		"). Chat UI / Hermes Gateway should run the L0 prompt; platform-api V1 does not spawn a remote Hermes session."
	if err := h.store.Append(insight); err != nil {
		slog.Warn("hermesinsight persist", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, RunFirstTaskResponse{OK: true, Insight: insight, Error: ""})
}

func parseLimit(raw string) int {
	limit := DefaultListLimit
	raw = strings.TrimSpace(raw)
	if raw != "" {
		n, err := strconv.Atoi(raw)
		if err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > MaxInsights {
		limit = MaxInsights
	}
	return limit
}

func blockersError(ready hermesreadiness.ReadinessResponse) string {
	if len(ready.Blockers) > 0 {
		return strings.Join(ready.Blockers, "; ")
	}
	return "Hermes / Nous not ready for first task"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
