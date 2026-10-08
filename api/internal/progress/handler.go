package progress

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute

type cached struct {
	resp Response
	at   time.Time
}

// Handler serves GET /api/v1/progress. Viewer and above: the route is a read,
// same as /api/v1/lineage (anonymous is the viewer seat).
type Handler struct {
	svc   *Service
	mu    sync.Mutex
	cache map[int]cached
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc, cache: map[int]cached{}}
}

// HandleGet returns computed progress.
//
//	?stuck_days=3   an in-progress item with no commit for this many days is stuck (1–30, default 3)
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("stuck_days"))
	n = clampStuck(n)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.get(r, n))
}

func (h *Handler) get(r *http.Request, stuckDays int) Response {
	h.mu.Lock()
	c, ok := h.cache[stuckDays]
	h.mu.Unlock()
	if ok && time.Since(c.at) < cacheTTL {
		return c.resp
	}
	resp := h.svc.Build(r.Context(), stuckDays)
	if len(resp.Errors) == 0 {
		h.mu.Lock()
		h.cache[stuckDays] = cached{resp: resp, at: time.Now()}
		h.mu.Unlock()
	}
	return resp
}
