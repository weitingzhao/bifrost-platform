// Package privilege records the Owner's uses of privilege on the bus
// (owner.privilege.used, 2026-11-21): each time the Owner steps in to push
// work that lines wait on, or that no contract gives to anyone. The Owner
// wants the count as low as possible and kept every day as a fixed
// reference; a count in a note is not a system archive (law 93), so the
// fact goes to the ledger and the evaluation readout counts it
// (owner_privilege_uses, the metrics line). The coordinator records the
// fact; the body's shape is declared in the register (rocket.yaml).
//
//	POST /api/v1/bus/privilege   {what, stuck_because, who?, cause?}: one use
package privilege

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Type is the registered event type; Stream the ledger stream of all uses.
const (
	Type   = "owner.privilege.used"
	Stream = "owner-privilege"
	Ref    = "/api/v1/readings/evaluation"
)

// Use is one use of privilege as the caller states it. Who is whose
// privilege it was (default "owner"); Cause, when known, is contract,
// owner_undecided or tool (Triage's judgement, which may come later).
type Use struct {
	Who          string `json:"who"`
	What         string `json:"what"`
	StuckBecause string `json:"stuck_because"`
	Cause        string `json:"cause"`
}

// Body is the event body of a use; the register checks its values.
func Body(u Use) map[string]any {
	who := strings.TrimSpace(u.Who)
	if who == "" {
		who = "owner"
	}
	b := map[string]any{"who": who, "subject": who, "what": strings.TrimSpace(u.What),
		"stuck_because": strings.TrimSpace(u.StuckBecause), "ref": Ref, "change": "used"}
	if c := strings.TrimSpace(u.Cause); c != "" {
		b["cause"] = c
	}
	return b
}

// Record appends one use as the writer; the event's id is returned.
func Record(ctx context.Context, db eventlog.DB, w actor.Writer, u Use) (int64, error) {
	var id int64
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := actor.Ensure(ctx, tx, w, false); err != nil {
			return err
		}
		a, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream, Type: Type, Actor: w.Actor, Body: Body(u)})
		id = a.EventID
		return err
	})
	return id, err
}

// API is the route's dependencies: the ledger (nil while it is not
// connected) and who the request writes as.
type API struct {
	DB  func() eventlog.DB
	Who func(*http.Request) (actor.Writer, error)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// HandleRecord is POST /bus/privilege.
func (a API) HandleRecord(w http.ResponseWriter, r *http.Request) {
	db := a.DB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger is not connected"})
		return
	}
	var u Use
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&u); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is {what, stuck_because, who?, cause?}: " + err.Error()})
		return
	}
	who, err := a.Who(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	id, err := Record(r.Context(), db, who, u)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": id, "type": Type, "by": who.Actor})
}
