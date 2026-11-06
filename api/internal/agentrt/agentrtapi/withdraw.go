package agentrtapi

import (
	"errors"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

type withdrawBody struct {
	IdempotencyKey string   `json:"idempotency_key"`
	TaskIDs        []string `json:"task_ids"`
	Reason         string   `json:"reason"`
}

// withdrawTasks is POST /tasks/withdraw: the proposer of unstarted tasks, or
// of their mission, cancels them (store.WithdrawTasks). A request that breaks
// a rule of its own is 400; a refusal is as fail says (not_yours 403, state
// 409).
func (a *API) withdrawTasks(w http.ResponseWriter, r *http.Request) {
	var b withdrawBody
	if !decode(w, r, &b) {
		return
	}
	who, err := actorOf(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.WithdrawTasks(r.Context(), a.DB, who, b.IdempotencyKey, b.TaskIDs, b.Reason)
	if errors.Is(err, store.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
