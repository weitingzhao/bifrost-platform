package agentprogress

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// Handler serves PUT and GET /api/v1/agent/progress.
type Handler struct {
	rec *Recorder
	now func() time.Time
}

func NewHandler(rec *Recorder) *Handler {
	return &Handler{rec: rec, now: time.Now}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// HandlePut is PUT /api/v1/agent/progress (reporter). The body is one JSON
// document. The server keeps only the latest.
func (h *Handler) HandlePut(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > MaxDocumentBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "document too large"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxDocumentBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body: " + err.Error()})
		return
	}
	if int64(len(body)) > MaxDocumentBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "document too large"})
		return
	}
	doc, host, err := prepare(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.rec.Publish(doc, host); err != nil {
		if errors.Is(err, errTooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "document too large"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

type getResponse struct {
	Document             json.RawMessage `json:"document"`
	ReceivedAt           string          `json:"received_at"`
	AgeSeconds           int64           `json:"age_seconds"`
	Stale                bool            `json:"stale"`
	Host                 string          `json:"host"`
	GeneratedAt          string          `json:"generated_at"`
	GenerationLagSeconds int64           `json:"generation_lag_seconds"`
}

// HandleGet is GET /api/v1/agent/progress (viewer). Nothing published yet is
// 200 with document null.
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	snap, ok, err := h.rec.Current()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, project(snap, ok, h.now().UTC()))
}

func project(snap Snapshot, ok bool, now time.Time) getResponse {
	if !ok || len(snap.Document) == 0 || string(snap.Document) == "null" {
		return getResponse{Document: json.RawMessage("null")}
	}
	age := now.Sub(snap.ReceivedAt)
	if age < 0 {
		age = 0
	}
	received := ""
	if !snap.ReceivedAt.IsZero() {
		received = snap.ReceivedAt.UTC().Format(time.RFC3339Nano)
	}
	resp := getResponse{
		Document:   snap.Document,
		ReceivedAt: received,
		AgeSeconds: int64(age / time.Second),
		Stale:      age > StaleAfter,
		Host:       snap.Host,
	}
	if gen, ok := generatedTime(snap.Document); ok {
		resp.GeneratedAt = gen.Format(time.RFC3339Nano)
		if !snap.ReceivedAt.IsZero() {
			resp.GenerationLagSeconds = int64(snap.ReceivedAt.Sub(gen) / time.Second)
		}
	}
	return resp
}
