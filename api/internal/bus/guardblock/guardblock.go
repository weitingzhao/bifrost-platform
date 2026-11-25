// Package guardblock records each refusal by the agent guard on the bus
// (guard.block.recorded, 2026-11-21). The guard (preflight.js, run on the
// Owner's machines before every tool call) refuses actions; how often each
// rule refuses whom was read only from session transcripts on one Mac, which
// is no system archive (LAW-8), and the wall list the law-reform seat reads
// needs it. A block carries the rule and the session, never the command:
// a command can hold paths and names that must not leave the machine.
//
//	POST /api/v1/bus/guard-blocks   {rule, session, host?, blocked_at?}: one refusal
//
// blocked_at is when the guard refused (the block log's first column): a
// reporter replays the log later, so the event's own time is when it was
// posted, and a reading by day needs the refusal's (2026-11-21, the metrics
// line).
package guardblock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Type is the registered event type; Stream the ledger stream of all blocks.
const (
	Type   = "guard.block.recorded"
	Stream = "guard-blocks"
	Ref    = "/api/v1/bus/events?type=guard.block.recorded"
)

// Rules are the kinds preflight.js refuses with (its denyMessage kinds).
var Rules = []string{"D10", "nas", "owner", "approval", "dev-services", "shared-worktree", "push-main", "gitea-primary"}

var (
	sessionR = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,80}$`)
	hostR    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,63}$`)
)

// Block is one refusal as the reporter states it.
type Block struct {
	Rule      string `json:"rule"`
	Session   string `json:"session"`
	Host      string `json:"host"`
	BlockedAt string `json:"blocked_at"`
}

// Body is the event body of a block, or why it is refused.
func Body(b Block) (map[string]any, error) {
	rule := strings.TrimSpace(b.Rule)
	known := false
	for _, r := range Rules {
		known = known || r == rule
	}
	if !known {
		return nil, fmt.Errorf("rule is one of %s", strings.Join(Rules, ", "))
	}
	s := strings.TrimSpace(b.Session)
	if s == "" {
		s = "-"
	}
	if s != "-" && !sessionR.MatchString(s) {
		return nil, fmt.Errorf("session is the session id, letters, digits and _.:- only")
	}
	out := map[string]any{"subject": rule, "rule": rule, "session": s, "ref": Ref, "change": "recorded"}
	if h := strings.TrimSpace(b.Host); h != "" {
		if !hostR.MatchString(h) {
			return nil, fmt.Errorf("host is a machine name, letters, digits and _.- only")
		}
		out["host"] = h
	}
	if at := strings.TrimSpace(b.BlockedAt); at != "" {
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, fmt.Errorf("blocked_at is an RFC 3339 time")
		}
		out["blocked_at"] = t.UTC().Format(time.RFC3339Nano)
	}
	return out, nil
}

// Record appends one block as the writer; the event's id is returned.
func Record(ctx context.Context, db eventlog.DB, w actor.Writer, b Block) (int64, error) {
	body, err := Body(b)
	if err != nil {
		return 0, err
	}
	var id int64
	err = eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := actor.Ensure(ctx, tx, w, false); err != nil {
			return err
		}
		a, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream, Type: Type, Actor: w.Actor, Body: body})
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

// HandleRecord is POST /bus/guard-blocks.
func (a API) HandleRecord(w http.ResponseWriter, r *http.Request) {
	db := a.DB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger is not connected"})
		return
	}
	var b Block
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is {rule, session, host?, blocked_at?}: " + err.Error()})
		return
	}
	who, err := a.Who(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	id, err := Record(r.Context(), db, who, b)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": id, "type": Type, "by": who.Actor})
}
