package agentrtapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// searchArtifacts lists artifacts' metadata, newest first (GET /artifacts,
// W-31 2.3 search_artifacts; the Owner 2026-10-21 「2.3 的四条都照推荐 A」):
// kind= one or more kinds, comma-separated; task= a task (t_…) or mission= a
// mission (M-n): the artifacts its records name; author= the actor that first
// stored it; since= an RFC 3339 instant; limit= 1 to 100 (default 50);
// before= the next of a previous answer. Never a body: GET /artifacts/{id}
// reads one, and an Agent host's token is shown no exam artifact here either.
func (a *API) searchArtifacts(w http.ResponseWriter, r *http.Request) {
	in := r.URL.Query()
	q := store.ArtifactQuery{Task: in.Get("task"), Mission: in.Get("mission"), Author: in.Get("author"), Before: in.Get("before"),
		HideExams: actuation.PrincipalFromContext(r.Context()).Role == actuation.RoleAgentd}
	if s := in.Get("kind"); s != "" {
		q.Kinds = strings.Split(s, ",")
	}
	if s := in.Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "since is an RFC 3339 instant, not "+strconv.Quote(s))
			return
		}
		q.Since = t
	}
	if s := in.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > store.ArtifactSearchLimit {
			writeError(w, http.StatusBadRequest, "bad_request", "limit is 1 to "+strconv.Itoa(store.ArtifactSearchLimit))
			return
		}
		q.Limit = n
	}
	list, next, err := store.SearchArtifacts(r.Context(), a.DB, q)
	if errors.Is(err, store.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": list, "next": next})
}
