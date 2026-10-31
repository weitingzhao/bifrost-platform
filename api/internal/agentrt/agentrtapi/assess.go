package agentrtapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// requestAssessment is an Owner-only opt-in. It never reads evidence, answers
// a question, decides an approval, or changes approval material.
func (a *API) requestAssessment(w http.ResponseWriter, r *http.Request) {
	if a.CheckAssessment == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "assessment item store is unavailable")
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	kind, id := chi.URLParam(r, "kind"), chi.URLParam(r, "id")
	err = store.RequestAssessment(r.Context(), a.DB, who, kind, id, func(ctx context.Context) error {
		return a.CheckAssessment(ctx, kind, id)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	raw, err := store.ReadAssessment(r.Context(), a.DB, kind, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
