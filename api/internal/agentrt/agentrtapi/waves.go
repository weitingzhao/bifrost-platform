package agentrtapi

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (a *API) openWave(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Repo     string `json:"repo"`
		PlanRoot string `json:"plan_root"`
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Repo != "bifrost-platform" {
		writeError(w, http.StatusBadRequest, "bad_request", "only bifrost-platform takes waves for now")
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.OpenWave(r.Context(), a.DB, who, b.Repo, b.PlanRoot, a.Head)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// endWave closes a wave (abandon false) or abandons it; one plain route
// each, so every route walker samples it like any other route.
func (a *API) endWave(abandon bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		who, err := writer(r)
		if err != nil {
			a.fail(w, err)
			return
		}
		if err = store.EndWave(r.Context(), a.DB, who, chi.URLParam(r, "id"), abandon); err != nil {
			a.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": chi.URLParam(r, "id")})
	}
}

func (a *API) listWaves(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo != "" && repo != "bifrost-platform" {
		writeError(w, http.StatusBadRequest, "bad_request", "only bifrost-platform takes waves for now")
		return
	}
	state := r.URL.Query().Get("state")
	if state != "" && state != "open" && state != "closed" && state != "abandoned" {
		writeError(w, http.StatusBadRequest, "bad_request", "state is open, closed or abandoned")
		return
	}
	out, err := store.Waves(r.Context(), a.DB, repo)
	if err != nil {
		a.fail(w, err)
		return
	}
	if state != "" {
		filtered := []store.Wave{}
		for _, wave := range out {
			if wave.State == state {
				filtered = append(filtered, wave)
			}
		}
		out = filtered
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].N > out[j].N })
	writeJSON(w, http.StatusOK, out)
}
