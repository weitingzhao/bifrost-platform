package cluster

import "net/http"

// DataCloneWrites are the two write handlers of the tenant's data clone
// (package dataclone, moved out of this package on 2026-11-17). The action
// wiring (server/actions_wire.go, trust-anchored) names them on this
// Handler, so they stay here as forwarders; the server binds them
// (BindDataClone). This package does not import dataclone.
type DataCloneWrites struct {
	Clone       http.HandlerFunc // POST /cluster/data-clone
	SchedulePut http.HandlerFunc // PUT /cluster/data-clone/schedule
}

// BindDataClone sets the handlers the two forwarders call.
func (h *Handler) BindDataClone(w DataCloneWrites) { h.dataClone = w }

// HandleDataClone forwards to the data clone's POST handler.
func (h *Handler) HandleDataClone(w http.ResponseWriter, r *http.Request) {
	forwardDataClone(w, r, h.dataClone.Clone)
}

// HandleDataCloneSchedulePut forwards to the data clone's schedule PUT handler.
func (h *Handler) HandleDataCloneSchedulePut(w http.ResponseWriter, r *http.Request) {
	forwardDataClone(w, r, h.dataClone.SchedulePut)
}

// forwardDataClone answers 503 when nothing is bound: no clone is started and
// no schedule is written.
func forwardDataClone(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if next == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "data clone is not wired to this server (cluster.BindDataClone)"})
		return
	}
	next(w, r)
}
