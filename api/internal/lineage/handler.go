package lineage

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute

type cached struct {
	resp Response
	at   time.Time
}

// Handler serves GET /api/v1/lineage.
type Handler struct {
	svc   *Service
	mu    sync.Mutex
	cache map[int]cached
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc, cache: map[int]cached{}}
}

// HandleGet returns agent threads and the commits they stamped.
//
//	?days=14        window (1–90)
//	?session=local_…  only this thread
//	?change_id=I…   only commits with this Change-Id (did this change land?)
//	?refresh=true   bypass the 5-minute cache
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	days = clampDays(days)
	resp := h.get(r, days, q.Get("refresh") == "true")
	resp = filter(resp, strings.TrimSpace(q.Get("session")), strings.TrimSpace(q.Get("change_id")))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) get(r *http.Request, days int, refresh bool) Response {
	h.mu.Lock()
	c, ok := h.cache[days]
	h.mu.Unlock()
	if ok && !refresh && time.Since(c.at) < cacheTTL {
		return c.resp
	}
	resp := h.svc.Build(r.Context(), days)
	if len(resp.Errors) == 0 || len(resp.Threads) > 0 {
		h.mu.Lock()
		h.cache[days] = cached{resp: resp, at: time.Now()}
		h.mu.Unlock()
	}
	return resp
}

// filter narrows a cached response without mutating it.
func filter(resp Response, session, changeID string) Response {
	if session == "" && changeID == "" {
		return resp
	}
	threads := make([]Thread, 0)
	for _, t := range resp.Threads {
		if session != "" && t.Session != session {
			continue
		}
		if changeID == "" {
			threads = append(threads, t)
			continue
		}
		nt := t
		nt.Repos = nil
		for _, rc := range t.Repos {
			var keep []Commit
			for _, c := range rc.Commits {
				if c.ChangeID == changeID {
					keep = append(keep, c)
				}
			}
			if len(keep) > 0 {
				nt.Repos = append(nt.Repos, RepoCommits{Repo: rc.Repo, Commits: keep})
			}
		}
		summarize(&nt)
		if nt.CommitCount > 0 {
			threads = append(threads, nt)
		}
	}
	resp.Threads = threads
	return resp
}
