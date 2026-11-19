package regressions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The known regressions on main (2026-11-17, the coordinator: information,
// no gate). Any seat records one or its fix, as itself; anyone with a viewer
// token reads them. The release context lists the open ones of its
// repository (the server reads them with Open).
//
//	GET  /api/v1/bus/regressions?repo=&open=1   the known regressions (open: unfixed only)
//	POST /api/v1/bus/regressions                {repo, introduced, impact}: one is known
//	POST /api/v1/bus/regressions/fixed          {repo, introduced, fixed}: it is fixed (409 when none is known)

// API is the routes' dependencies: the ledger (nil while it is not
// connected) and who the request writes as.
type API struct {
	DB  func() eventlog.DB
	Who func(*http.Request) (actor.Writer, error)
}

type reqBody struct {
	Repo       string `json:"repo"`
	Introduced string `json:"introduced"`
	Impact     string `json:"impact"`
	Fixed      string `json:"fixed"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// HandleList is GET /bus/regressions.
func (a API) HandleList(w http.ResponseWriter, r *http.Request) {
	db := a.DB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger is not connected"})
		return
	}
	list, err := List(r.Context(), db, r.URL.Query().Get("repo"), r.URL.Query().Get("open") == "1")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"regressions": list})
}

// HandleWrite is POST /bus/regressions (fixed false) or /bus/regressions/fixed.
func (a API) HandleWrite(fixed bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := a.DB()
		if db == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger is not connected"})
			return
		}
		var b reqBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is {repo, introduced, impact} or {repo, introduced, fixed}: " + err.Error()})
			return
		}
		who, err := a.Who(r)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		var id int64
		if fixed {
			id, err = Fixed(r.Context(), db, who, b.Repo, b.Introduced, b.Fixed)
		} else {
			id, err = Known(r.Context(), db, who, b.Repo, b.Introduced, b.Impact)
		}
		switch {
		case errors.Is(err, ErrNotKnown):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case err != nil:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusCreated, map[string]any{"event": id, "key": Key(b.Repo, b.Introduced), "by": who.Actor})
		}
	}
}

// Open is repo's known regressions on main with no fix recorded, newest
// first (the release context's reader).
func (a API) Open(ctx context.Context, repo string) ([]Regression, error) {
	db := a.DB()
	if db == nil {
		return nil, errors.New("the ledger is not connected")
	}
	return List(ctx, db, repo, true)
}

// OpenDetail is a wake's detail regressions:open: the known regressions on
// main still open, a line each ("" when none, so a row that requires it
// wakes nobody).
func (a API) OpenDetail(ctx context.Context) (map[string]string, error) {
	open, err := a.Open(ctx, "")
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(open))
	for _, g := range open {
		lines = append(lines, fmt.Sprintf("%s@%s: %s (%s)", g.Repo, g.Introduced[:12], g.Impact, g.By))
	}
	return map[string]string{"open": strings.Join(lines, "; ")}, nil
}
