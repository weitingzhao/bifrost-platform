package briefing

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/promote"
	"github.com/weitingzhao/bifrost-platform/api/internal/sessionsnapshot"
)

type Handler struct {
	cfg          *config.Config
	prober       *probe.Prober
	snapshots    *sessionsnapshot.Store
	results      *SessionResultStore
	audit        *actuation.AuditLog
	promoteStore *promote.Store
}

func NewHandler(cfg *config.Config, prober *probe.Prober, audit *actuation.AuditLog, promoteStore *promote.Store) *Handler {
	return &Handler{
		cfg:          cfg,
		prober:       prober,
		snapshots:    sessionsnapshot.NewStore(),
		results:      NewSessionResultStore(),
		audit:        audit,
		promoteStore: promoteStore,
	}
}

func (h *Handler) overlayContext() *opscontext.File {
	if h.cfg.OpsContext == nil {
		return nil
	}
	if h.promoteStore != nil {
		return promote.OverlayContext(h.cfg.OpsContext, h.promoteStore)
	}
	return h.cfg.OpsContext
}

func (h *Handler) HandleSessionPack(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := PackRequest{
		Track:    q.Get("track"),
		Lane:     q.Get("lane"),
		Intent:   q.Get("intent"),
		PackSize: q.Get("pack"),
	}

	ctx := h.overlayContext()
	matrices := h.probeAllMatrices(r)
	baselineAt := ""
	if env, ok := h.snapshots.Latest(); ok {
		baselineAt = env.SavedAt
	}

	resp := BuildSessionPack(ctx, matrices, "unknown", "", baselineAt, req)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleListSessionResults(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"results": h.results.List(50),
	})
}

type sessionResultBody struct {
	JobID              string `json:"job_id"`
	Outcome            string `json:"outcome"`
	Summary            string `json:"summary"`
	Track              string `json:"track"`
	Lane               string `json:"lane"`
	Intent             string `json:"intent"`
	SpineNote          string `json:"spine_note"`
	RequestSpineUpdate bool   `json:"request_spine_update"`
}

func (h *Handler) HandleCloseSession(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
		return
	}
	var req sessionResultBody
	if json.Unmarshal(body, &req) != nil || req.Outcome == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "outcome required"})
		return
	}
	principal := actuation.PrincipalFromContext(r.Context())
	record := SessionResult{
		ID:        newSessionResultID(),
		ClosedAt:  time.Now().UTC(),
		ClosedBy:  principal.Name,
		JobID:     req.JobID,
		Outcome:   req.Outcome,
		Summary:   req.Summary,
		Track:     req.Track,
		Lane:      req.Lane,
		Intent:    req.Intent,
		SpineNote: req.SpineNote,
	}
	if err := h.results.Append(record); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	detail := req.Summary
	if req.RequestSpineUpdate && req.SpineNote != "" {
		detail += " | spine_note: " + req.SpineNote
	}
	if h.audit != nil {
		h.audit.Record(r, "briefing.session.close", record.ID, req.Outcome, detail)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"result": record,
	})
}

func (h *Handler) probeAllMatrices(r *http.Request) []probe.MatrixResponse {
	if h.prober == nil || h.cfg == nil {
		return nil
	}
	ctx := r.Context()
	results := make([]probe.MatrixResponse, len(h.cfg.Environments))
	var wg sync.WaitGroup
	for i, env := range h.cfg.Environments {
		wg.Add(1)
		go func(idx int, e config.Environment) {
			defer wg.Done()
			results[idx] = h.prober.ProbeEnvironment(ctx, e)
		}(i, env)
	}
	wg.Wait()
	return results
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
