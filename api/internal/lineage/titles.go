package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// TitleAt is a title and when it was last seen.
type TitleAt struct {
	Title string
	At    time.Time
}

// ThreadTitles: Manual is session -> title set by hand; Transcript is transcript id -> session title.
type ThreadTitles struct {
	Manual     map[string]TitleAt
	Transcript map[string]TitleAt
}

// TitlesFunc loads thread titles (the threadtitles store, adapted by the server).
type TitlesFunc func(ctx context.Context) (ThreadTitles, error)

// SetTitleFunc names a thread by hand; an empty title clears the hand-set name.
type SetTitleFunc func(ctx context.Context, session, title string) error

// ReportTitleFunc records the title a session reported for its own transcript.
type ReportTitleFunc func(ctx context.Context, transcript, title string) error

// WithTitles names threads in every response and enables the rename endpoint.
func (h *Handler) WithTitles(get TitlesFunc, set SetTitleFunc) *Handler {
	h.titles, h.setTitle = get, set
	return h
}

// WithTitleReports enables PUT /api/v1/lineage/transcript-title.
func (h *Handler) WithTitleReports(report ReportTitleFunc) *Handler {
	h.reportTitle = report
	return h
}

type reportTitleRequest struct {
	Transcript string `json:"transcript"`
	Title      string `json:"title"`
}

// HandleReportTitle is PUT /api/v1/lineage/transcript-title {transcript, title}
// (reporter): a session reports its own current title — the user's rename or the
// generated one — so threads get named from any machine, as soon as the session
// next stops, without the workstation syncer.
func (h *Handler) HandleReportTitle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.reportTitle == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "thread titles not configured"})
		return
	}
	var req reportTitleRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "body: " + err.Error()})
		return
	}
	if err := h.reportTitle(r.Context(), req.Transcript, req.Title); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "transcript": req.Transcript})
}

// name returns a copy of resp with titles on its threads (resp may be cached; never mutate it).
func name(resp Response, tt ThreadTitles) Response {
	threads := make([]Thread, len(resp.Threads))
	for i, t := range resp.Threads {
		if m, ok := tt.Manual[t.Session]; ok && t.Session != "" {
			t.Title, t.TitleSource = m.Title, "manual"
		} else {
			// a thread can span several transcripts (compaction, fork): the most recently seen title wins
			var best TitleAt
			for _, tr := range t.Transcripts {
				if e, ok := tt.Transcript[tr]; ok && e.At.After(best.At) {
					best = e
				}
			}
			if best.Title != "" {
				t.Title, t.TitleSource = best.Title, "transcript"
			}
		}
		threads[i] = t
	}
	resp.Threads = threads
	return resp
}

type setTitleRequest struct {
	Session string `json:"session"`
	Title   string `json:"title"`
}

// HandleSetTitle is PUT /api/v1/lineage/thread-title {session, title} (operator).
// An empty title removes the hand-set name, falling back to the synced session title.
func (h *Handler) HandleSetTitle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.setTitle == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "thread titles not configured"})
		return
	}
	var req setTitleRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "body: " + err.Error()})
		return
	}
	if err := h.setTitle(r.Context(), req.Session, req.Title); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "session": req.Session, "title": req.Title})
}
