package releases

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler serves GET /api/v1/releases.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Service() *Service { return h.svc }

type listResponse struct {
	Status  Status   `json:"status"`
	Records []Record `json:"records"`
	// Total is how many records match ?lane= and ?env=, all pages together.
	Total int `json:"total"`
	// Next is the ?before= of the next page, older records; "" on the last.
	Next  string `json:"next,omitempty"`
	Error string `json:"error,omitempty"`
}

// HandleList returns release records, newest first. ?lane= ?env= filter;
// ?limit= (default 100); ?before=<run> starts after that run's record, so a
// reader pages back with the answer's next. total counts every record the
// filter matches (2026-11-23, the Console's ask: PROD keeps 1,673 records,
// 1.1 MB read whole, and the History table could not say how many there
// are). A ?before= that names no record of the filter is an error, not the
// first page.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	before := q.Get("before")
	resp := listResponse{Status: h.svc.Status(), Records: []Record{}}
	recs, err := h.svc.List(r.Context())
	if err != nil {
		resp.Error = err.Error()
	}
	started, more := before == "", false
	for _, rec := range recs {
		if (q.Get("lane") != "" && rec.Lane != q.Get("lane")) || (q.Get("env") != "" && rec.Env != q.Get("env")) {
			continue
		}
		resp.Total++
		if !started {
			started = rec.Run == before
			continue
		}
		if len(resp.Records) >= limit {
			more = true
			continue
		}
		resp.Records = append(resp.Records, rec)
	}
	if !started && err == nil {
		resp.Error = "before: no record of run " + before + " matches this filter"
	}
	if more && len(resp.Records) > 0 {
		resp.Next = resp.Records[len(resp.Records)-1].Run
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleRunningImages returns configured live image tags. See config/running-images.yaml.
func (h *Handler) HandleRunningImages(w http.ResponseWriter, r *http.Request) {
	cells, err := h.svc.RunningImages(r.Context())
	body := map[string]any{"cells": cells}
	if err != nil {
		body["error"] = err.Error()
		if cells == nil {
			body["cells"] = []ImageCell{}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
