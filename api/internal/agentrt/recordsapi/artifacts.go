package recordsapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// HandleSearchArtifacts lists artifacts' metadata, newest first (GET /artifacts,
// W-31 2.3 search_artifacts; 决定 2026-10-21):
// kind= one or more kinds, comma-separated; task= a task (t_…) or mission= a
// mission (M-n): the artifacts its records name; author= the actor that first
// stored it; since= an RFC 3339 instant; limit= 1 to 100 (default 50);
// before= the next of a previous answer. Never a body: GET /artifacts/{id}
// reads one, and an Agent host's token is shown no exam artifact here either.
func (rt Routes) HandleSearchArtifacts(w http.ResponseWriter, r *http.Request) {
	in := r.URL.Query()
	q := artifact.ArtifactQuery{Task: in.Get("task"), Mission: in.Get("mission"), Author: in.Get("author"), Before: in.Get("before"),
		HideExams: actuation.PrincipalFromContext(r.Context()).Role == actuation.RoleAgentd}
	if s := in.Get("kind"); s != "" {
		q.Kinds = strings.Split(s, ",")
	}
	if s := in.Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			rt.Host.Error(w, http.StatusBadRequest, "bad_request", "since is an RFC 3339 instant, not "+strconv.Quote(s))
			return
		}
		q.Since = t
	}
	if s := in.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > artifact.ArtifactSearchLimit {
			rt.Host.Error(w, http.StatusBadRequest, "bad_request", "limit is 1 to "+strconv.Itoa(artifact.ArtifactSearchLimit))
			return
		}
		q.Limit = n
	}
	list, next, err := artifact.SearchArtifacts(r.Context(), rt.Host.DB(), q)
	if errors.Is(err, eventlog.ErrInvalid) {
		rt.Host.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"artifacts": list, "next": next})
}
