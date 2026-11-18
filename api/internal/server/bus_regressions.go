package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// The known regressions on main (2026-11-17, the coordinator: information,
// no gate; bus/regressions). Any seat records one or its fix, as itself;
// anyone with a viewer token reads them. The release context lists the open
// ones of its repository (openRegressions).
//
//	GET  /api/v1/bus/regressions?repo=&open=1   the known regressions (open: unfixed only)
//	POST /api/v1/bus/regressions                {repo, introduced, impact}: one is known
//	POST /api/v1/bus/regressions/fixed          {repo, introduced, fixed}: it is fixed (409 when none is known)

type regressionBody struct {
	Repo       string `json:"repo"`
	Introduced string `json:"introduced"`
	Impact     string `json:"impact"`
	Fixed      string `json:"fixed"`
}

func (s *Server) handleRegressions(w http.ResponseWriter, r *http.Request) {
	db := s.bus.db()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger is not connected"})
		return
	}
	list, err := regressions.List(r.Context(), db, r.URL.Query().Get("repo"), r.URL.Query().Get("open") == "1")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"regressions": list})
}

func (s *Server) handleRegressionWrite(fixed bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := s.bus.db()
		if db == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger is not connected"})
			return
		}
		var b regressionBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is {repo, introduced, impact} or {repo, introduced, fixed}: " + err.Error()})
			return
		}
		who, err := agentrtapi.WriterOf(r)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		var id int64
		if fixed {
			id, err = regressions.Fixed(r.Context(), db, who, b.Repo, b.Introduced, b.Fixed)
		} else {
			id, err = regressions.Known(r.Context(), db, who, b.Repo, b.Introduced, b.Impact)
		}
		switch {
		case errors.Is(err, regressions.ErrNotKnown):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case err != nil:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusCreated, map[string]any{"event": id, "key": regressions.Key(b.Repo, b.Introduced), "by": who.Actor})
		}
	}
}

// openRegressions is the release context's reader: repo's known regressions
// on main with no fix recorded, newest first.
func (s *Server) openRegressions(ctx context.Context, repo string) ([]delivery.KnownRegression, error) {
	db := s.bus.db()
	if db == nil {
		return nil, errors.New("the ledger is not connected")
	}
	open, err := regressions.List(ctx, db, repo, true)
	if err != nil {
		return nil, err
	}
	return knownRegressions(open), nil
}

func knownRegressions(open []regressions.Regression) []delivery.KnownRegression {
	out := make([]delivery.KnownRegression, 0, len(open))
	for _, g := range open {
		out = append(out, delivery.KnownRegression{Introduced: g.Introduced, Impact: g.Impact, By: g.By, KnownAt: g.KnownAt.UTC().Format(time.RFC3339)})
	}
	return out
}

// openRegressionsDetail is a wake's detail regressions:open: the known
// regressions on main still open, a line each ("" when none, so a row that
// requires it wakes nobody).
func (s *Server) openRegressionsDetail(ctx context.Context) (map[string]string, error) {
	db := s.bus.db()
	if db == nil {
		return nil, errors.New("the ledger is not connected")
	}
	open, err := regressions.List(ctx, db, "", true)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(open))
	for _, g := range open {
		lines = append(lines, fmt.Sprintf("%s@%s: %s (%s)", g.Repo, g.Introduced[:12], g.Impact, g.By))
	}
	return map[string]string{"open": strings.Join(lines, "; ")}, nil
}
