package agentrtapi

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// claimPinnedRequest is POST /agentrt/claim/pinned (DESIGN-coordinator-
// landing-v3 §2.4, §2.6): the task it names, the key a retry repeats, the
// host's free slots and disk. It takes no tools: the standing envelope's
// entry for the pin says which executor and provider run the task.
type claimPinnedRequest struct {
	hostBody
	TaskID         string `json:"task_id"`
	IdempotencyKey string `json:"idempotency_key"`
	FreeSlots      int    `json:"free_slots"`
	DiskFree       *int64 `json:"disk_free_bytes,omitempty"`
	RootFree       *int64 `json:"root_free_bytes,omitempty"`
}

// claimPinned is the exact claim of the external host: one task, by id,
// once per key (store.ClaimPinned). It answers at once: the task is leased
// (200, the same answer to a retry with the key), or why not (403 for a
// host or task that is not the pin's, 409 with the reason for one that
// cannot be leased now).
func (a *API) claimPinned(w http.ResponseWriter, r *http.Request) {
	var req claimPinnedRequest
	if !decode(w, r, &req) || !boundHost(w, r, req.HostID) {
		return
	}
	cr := store.PinnedClaimRequest{HostID: req.HostID, TaskID: req.TaskID, Key: req.IdempotencyKey, FreeSlots: req.FreeSlots,
		DiskFloor: a.DiskFloor, Token: actuation.PrincipalFromContext(r.Context()).Name}
	switch {
	case (req.DiskFree == nil) != (req.RootFree == nil):
		writeError(w, http.StatusBadRequest, "bad_request", "disk_free_bytes and root_free_bytes go together")
		return
	case req.DiskFree != nil && (*req.DiskFree < 0 || *req.RootFree < 0):
		writeError(w, http.StatusBadRequest, "bad_request", "free disk is not negative")
		return
	case req.DiskFree != nil:
		cr.Disk = &store.HostDisk{Host: *req.DiskFree, Root: *req.RootFree}
	}
	out, err := store.ClaimPinned(r.Context(), a.DB, cr)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
