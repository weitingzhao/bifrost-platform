// Package acceptance records each reading of the decoupling acceptance on
// the bus (foundation.acceptance.measured, 2026-11-21). The Owner wanted
// Foundation's state on the coordination bus, whoever consumes it: the ten
// items' reading lived only in the Foundation seat's notes and its script's
// output, which is no system archive (law 93). The reading carries the
// ruler's version, because the ruler is Foundation's own script and changed
// three times on 2026-11-21; a score without its version is not comparable.
//
//	POST /api/v1/bus/acceptance   {ruler, score, passed, items, platform_commit, read_at}: one reading
package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Type is the registered event type; Stream the ledger stream of all readings.
const (
	Type   = "foundation.acceptance.measured"
	Stream = "foundation-acceptance"
	Ref    = "/api/v1/bus/events?type=foundation.acceptance.measured"
)

var (
	sha40   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	score   = regexp.MustCompile(`^(10|[0-9])/10$`)
	statusR = regexp.MustCompile(`^(pass|fail|unmeasured)$`)
)

// Reading is one run of the acceptance script as its producer states it.
// Items is the status of items 1 to 10, in order.
type Reading struct {
	Ruler          int      `json:"ruler"`
	Score          string   `json:"score"`
	Items          []string `json:"items"`
	PlatformCommit string   `json:"platform_commit"`
	ReadAt         string   `json:"read_at"`
}

// Body is the event body of a reading, or why it is refused.
func Body(rd Reading) (map[string]any, error) {
	if rd.Ruler < 1 || rd.Ruler > 99 {
		return nil, fmt.Errorf("ruler is the acceptance script's version, 1 to 99")
	}
	if !score.MatchString(rd.Score) {
		return nil, fmt.Errorf("score is n/10")
	}
	if len(rd.Items) != 10 {
		return nil, fmt.Errorf("items is the status of the ten items, in order")
	}
	passed := 0
	for i, s := range rd.Items {
		if !statusR.MatchString(s) {
			return nil, fmt.Errorf("item %d: status is pass, fail or unmeasured", i+1)
		}
		if s == "pass" {
			passed++
		}
	}
	if want := fmt.Sprintf("%d/10", passed); want != rd.Score {
		return nil, fmt.Errorf("score %s does not match the items (%s)", rd.Score, want)
	}
	if !sha40.MatchString(rd.PlatformCommit) {
		return nil, fmt.Errorf("platform_commit is a 40-character sha")
	}
	return map[string]any{
		"subject":         fmt.Sprintf("ruler-%d@%s", rd.Ruler, rd.PlatformCommit),
		"ruler":           rd.Ruler,
		"score":           rd.Score,
		"items":           strings.Join(rd.Items, ","),
		"platform_commit": rd.PlatformCommit,
		"read_at":         strings.TrimSpace(rd.ReadAt),
		"ref":             Ref,
		"change":          "measured",
	}, nil
}

// Record appends one reading as the writer; the event's id is returned.
func Record(ctx context.Context, db eventlog.DB, w actor.Writer, rd Reading) (int64, error) {
	body, err := Body(rd)
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

// HandleRecord is POST /bus/acceptance.
func (a API) HandleRecord(w http.ResponseWriter, r *http.Request) {
	db := a.DB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger is not connected"})
		return
	}
	var rd Reading
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rd); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is {ruler, score, items, platform_commit, read_at}: " + err.Error()})
		return
	}
	who, err := a.Who(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	id, err := Record(r.Context(), db, who, rd)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": id, "type": Type, "by": who.Actor})
}
