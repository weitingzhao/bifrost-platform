package codehealth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

const neverReportedNote = "no code-health report has ever been submitted — " +
	"treat as NOT OBSERVED, never as healthy. Run `make check-code-health` " +
	"in bifrost-trade-infra, or let CI report it."

type Handler struct {
	store *Store
	audit *actuation.AuditLog
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
	if !ok {
		writeJSON(w, http.StatusOK, StatusResponse{Reported: false, Note: neverReportedNote})
		return
	}
	writeJSON(w, http.StatusOK, StatusResponse{
		Reported: true,
		Latest:   latest,
		History:  h.store.List(limit),
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
