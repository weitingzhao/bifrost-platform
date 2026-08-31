package codehealth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

const neverReportedNote = "no code-health report has ever been submitted — " +
	"treat as NOT OBSERVED, never as healthy. Run Live Re-scan from the Console " +
	"(DEV), or `bash agent-config/scripts/code-health/scan.sh --report`."

type Handler struct {
	store *Store
	audit *actuation.AuditLog
	mu    sync.Mutex // serialise live rescans (scan.sh is CPU/IO heavy)
}

func NewHandler(audit *actuation.AuditLog) *Handler {
	return &Handler{store: NewStore(), audit: audit}
}

// HandleGet returns the latest reading plus recent history for trend rendering.
//
// With nothing stored it returns Reported=false and a note, not an empty
// success payload: absence of data must read as unobserved, not as clean.
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("history")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= historyLimit {
			limit = n
		}
	}

	latest, ok := h.store.Latest()
	fresh := buildFreshness(nil)
	if ok {
		fresh = buildFreshness(latest)
	}

	if !ok {
		writeJSON(w, http.StatusOK, StatusResponse{
			Reported:  false,
			Note:      neverReportedNote,
			Freshness: &fresh,
		})
		return
	}
	writeJSON(w, http.StatusOK, StatusResponse{
		Reported:  true,
		Latest:    latest,
		History:   h.store.List(limit),
		Freshness: &fresh,
	})
}

// HandleReport ingests one scan.sh run. Operator-gated: a reading that anyone
// could overwrite is a reading nobody can trust.
func (h *Handler) HandleReport(w http.ResponseWriter, r *http.Request) {
	var rep Report
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if len(rep.Metrics) == 0 {
		// A metric-less report would silently replace a real reading with an
		// empty one, which is the fake-green path scan.sh already refuses to
		// produce. Refuse to store it here too.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "metrics required — a report with no metric is not a passing report"})
		return
	}
	if strings.TrimSpace(rep.Commit) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit required — a reading with no commit cannot be checked for staleness"})
		return
	}

	rep.ReceivedAt = time.Now().UTC()
	if rep.Source == "" {
		rep.Source = "local"
	}
	if err := h.store.Put(rep); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	over := 0
	for _, m := range rep.Metrics {
		if m.Status == "over" {
			over++
		}
	}
	if h.audit != nil {
		h.audit.Record(r, "code-health.report", rep.Commit, "stored",
			fmt.Sprintf("metrics=%d over=%d source=%s", len(rep.Metrics), over, rep.Source))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stored":        true,
		"commit":        rep.Commit,
		"metrics":       len(rep.Metrics),
		"over_baseline": over,
	})
}

// HandleRescan runs scan.sh against the local workspace and stores the reading.
// Operator-gated. This is the Live Re-scan path for DEV Inner Loop — not an LLM.
func (h *Handler) HandleRescan(w http.ResponseWriter, r *http.Request) {
	if !h.mu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "a live re-scan is already running — wait for it to finish",
		})
		return
	}
	defer h.mu.Unlock()

	rep, stderr, err := runLiveScan(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":  err.Error(),
			"stderr": truncate(string(stderr), 4000),
		})
		return
	}
	if err := h.store.Put(rep); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	over := 0
	for _, m := range rep.Metrics {
		if m.Status == "over" {
			over++
		}
	}
	if h.audit != nil {
		h.audit.Record(r, "code-health.rescan", rep.Commit, "stored",
			fmt.Sprintf("metrics=%d over=%d source=live-rescan", len(rep.Metrics), over))
	}

	fresh := buildFreshness(&rep)
	writeJSON(w, http.StatusOK, map[string]any{
		"stored":        true,
		"commit":        rep.Commit,
		"metrics":       len(rep.Metrics),
		"over_baseline": over,
		"source":        rep.Source,
		"received_at":   rep.ReceivedAt,
		"freshness":     fresh,
		"latest":        rep,
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
