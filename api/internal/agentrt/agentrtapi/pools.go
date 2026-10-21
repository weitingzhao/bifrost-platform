package agentrtapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// listPools is GET /pools: every vendor pool with its reading, cooldown, pace
// and what the ledger charged it this window (W-31 2.4 slice ①).
func (a *API) listPools(w http.ResponseWriter, r *http.Request) {
	pools, err := store.ListPools(r.Context(), a.DB)
	if err != nil {
		a.fail(w, err)
		return
	}
	if pools == nil {
		pools = []store.PoolView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pools": pools})
}

type readingBody struct {
	IdempotencyKey string     `json:"idempotency_key"`
	UsedFraction   *float64   `json:"used_fraction"`
	ResetsAt       *time.Time `json:"resets_at"`
}

// recordReading is POST /pools/{id}/reading: the Owner's reading of a pool
// from the vendor's usage page (Owner 2026-10-18 「额度自动加手填」).
func (a *API) recordReading(w http.ResponseWriter, r *http.Request) {
	var b readingBody
	if !decode(w, r, &b) {
		return
	}
	who, err := actorOf(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	// A browser's encodeURIComponent sends claude%3Aowner%3Aweekly: Mount's
	// pathParams has unescaped it once (reviews 281 A1, 285).
	pool := chi.URLParam(r, "id")
	out, err := store.RecordOwnerReading(r.Context(), a.DB, who, b.IdempotencyKey,
		store.PoolReading{Pool: pool, UsedFraction: b.UsedFraction, ResetsAt: b.ResetsAt})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
