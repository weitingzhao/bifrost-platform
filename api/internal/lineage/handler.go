package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// cacheTTL is how long a build is fresh. Past it and up to staleMax a
// request is answered from the old build while one rebuild runs in the
// background; past staleMax (or with no build yet, or ?refresh=true) the
// request waits for the build. A cold build reads every repository
// (13.9 s, 884 KB measured 2026-11-19) and is shared by concurrent callers.
const (
	cacheTTL = 5 * time.Minute
	staleMax = time.Hour
)

type cached struct {
	resp Response
	at   time.Time
}

// Handler serves GET /api/v1/lineage.
type Handler struct {
	svc         *Service
	mu          sync.Mutex
	cache       map[int]cached
	titles      TitlesFunc
	setTitle    SetTitleFunc
	reportTitle ReportTitleFunc
	branches    branchesCache
	builds      singleflight.Group
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc, cache: map[int]cached{}}
}

// HandleGet returns agent threads and the commits they stamped.
//
//	?days=14        window (1–90)
//	?session=local_…  only this thread
//	?change_id=I…   only commits with this Change-Id (did this change land?)
//	?task=t_…       only commits agentd pushed for this agent runtime task
//	?refresh=true   bypass the 5-minute cache
//	?graph=true     include every repo's commit graph (default branch, open branches, release markers)
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	days = clampDays(days)
	resp := h.get(r, days, q.Get("refresh") == "true")
	resp = filter(resp, strings.TrimSpace(q.Get("session")), strings.TrimSpace(q.Get("change_id")), strings.TrimSpace(q.Get("task")))
	if q.Get("graph") != "true" {
		resp.Graph = nil
	}
	if h.titles != nil {
		// read per request, not cached: a rename shows on the next load
		if tt, err := h.titles(r.Context()); err == nil {
			resp = name(resp, tt)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) get(r *http.Request, days int, refresh bool) Response {
	h.mu.Lock()
	c, ok := h.cache[days]
	h.mu.Unlock()
	age := time.Since(c.at)
	switch {
	case ok && !refresh && age < cacheTTL:
		return c.resp
	case ok && !refresh && age < staleMax:
		// stale-while-revalidate: answer now, rebuild once in the background
		go func() {
			defer safego.Recover("lineage.rebuild")
			_, _, _ = h.build(days)
		}()
		return c.resp
	}
	resp, _, _ := h.build(days)
	return resp.(Response)
}

// build runs one Build per window at a time; callers that arrive while it
// runs share its result. It is detached from the request so that a caller
// who gives up does not cancel the build the others are waiting for.
func (h *Handler) build(days int) (interface{}, error, bool) {
	return h.builds.Do(strconv.Itoa(days), func() (interface{}, error) {
		resp := h.svc.Build(context.Background(), days) // Build bounds itself (buildTimeout)
		if len(resp.Errors) == 0 || len(resp.Threads) > 0 {
			h.mu.Lock()
			h.cache[days] = cached{resp: resp, at: time.Now()}
			h.mu.Unlock()
		}
		return resp, nil
	})
}

// filter narrows a cached response without mutating it.
func filter(resp Response, session, changeID, task string) Response {
	if session == "" && changeID == "" && task == "" {
		return resp
	}
	threads := make([]Thread, 0)
	for _, t := range resp.Threads {
		if session != "" && t.Session != session {
			continue
		}
		if changeID == "" && task == "" {
			threads = append(threads, t)
			continue
		}
		nt := t
		nt.Repos = nil
		for _, rc := range t.Repos {
			var keep []Commit
			for _, c := range rc.Commits {
				if (changeID == "" || c.ChangeID == changeID) && (task == "" || c.Task == task) {
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
