// Package states answers GET /api/v1/bus/states: each subject's latest state
// for one registered state-change type, read from the ledger.
//
// 2026-11-20, the Owner: every module's state changes reach the bus, and a
// module that joins must not mean a code change. A type qualifies by its
// register entry alone (registry/<tenant>.yaml: subject_from and
// change_from), so the next state type (a database, a market-data feed) is
// served here with no Go. The ledger keeps only changes, so a subject that
// has not changed within the type's retention is not listed; the type's ref
// is the full current list.
package states

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// MaxWindow bounds how far back a read reaches, whatever the retention says:
// the ledger's database is shared and a read must name its time range.
const MaxWindow = 30 * 24 * time.Hour

// Deps is what the read needs.
type Deps struct {
	DB func() eventlog.DB
	// Load reads the register; nil means registry.Load.
	Load func() (map[string]registry.Tenant, error)
}

// State is one subject's latest event of the type.
type State struct {
	Subject string            `json:"subject"`
	State   map[string]string `json:"state"`
	At      time.Time         `json:"at"`
	Event   string            `json:"event"`
}

// Response is the read's answer.
type Response struct {
	Type   string    `json:"type"`
	Tenant string    `json:"tenant"`
	Key    string    `json:"key"`
	Fields []string  `json:"fields"`
	Ref    string    `json:"ref"`
	Since  time.Time `json:"since"`
	Note   string    `json:"note"`
	States []State   `json:"states"`
}

const note = "Each subject's latest change within the window; a subject with no change in it is not listed. The ref is the full current list."

// Qualifies is the register entry of a type that can be read here, or why not.
func Qualifies(tenants map[string]registry.Tenant, tenant, typ string) (registry.Channel, error) {
	t, ok := tenants[tenant]
	if !ok {
		return registry.Channel{}, errors.New("no tenant " + strconv.Quote(tenant) + " in the register")
	}
	c, ok := t.Channels[typ]
	if !ok {
		return registry.Channel{}, errors.New(strconv.Quote(typ) + " is not a registered type of " + tenant)
	}
	if c.SubjectFrom == "" || len(c.ChangeFrom) == 0 {
		return registry.Channel{}, errors.New(strconv.Quote(typ) + " is not a state type: its register entry has no subject_from and change_from")
	}
	return c, nil
}

// Window is how far back a type is read: its retention, at most MaxWindow.
func Window(retention string) time.Duration {
	if len(retention) < 2 {
		return MaxWindow
	}
	n, err := strconv.Atoi(retention[:len(retention)-1])
	if err != nil || n <= 0 {
		return MaxWindow
	}
	var d time.Duration
	switch retention[len(retention)-1] {
	case 'd':
		d = time.Duration(n) * 24 * time.Hour
	case 'h':
		d = time.Duration(n) * time.Hour
	default:
		return MaxWindow
	}
	if d > MaxWindow {
		return MaxWindow
	}
	return d
}

// FromRows turns the ledger's latest rows into states, sorted by subject.
func FromRows(c registry.Channel, rows []eventlog.Row) []State {
	out := make([]State, 0, len(rows))
	for _, r := range rows {
		subject, _ := r.Body[c.SubjectFrom].(string)
		if subject == "" {
			continue
		}
		st := map[string]string{}
		for _, f := range c.ChangeFrom {
			if v, ok := r.Body[f]; ok && v != nil {
				st[f] = text(v)
			}
		}
		out = append(out, State{Subject: subject, State: st, At: r.At.UTC(), Event: relay.EventID(r.ID)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// Handle serves GET /api/v1/bus/states?type=<type>[&tenant=rocket].
func (d Deps) Handle(w http.ResponseWriter, r *http.Request) {
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	tenant := strings.TrimSpace(r.URL.Query().Get("tenant"))
	if tenant == "" {
		tenant = "rocket"
	}
	if typ == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type is required: a registered state type, e.g. cluster.workload.changed"})
		return
	}
	load := d.Load
	if load == nil {
		load = registry.Load
	}
	tenants, err := load()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the register does not load: " + err.Error()})
		return
	}
	c, err := Qualifies(tenants, tenant, typ)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var db eventlog.DB
	if d.DB != nil {
		db = d.DB()
	}
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger is not connected in this process"})
		return
	}
	since := time.Now().UTC().Add(-Window(c.Retention))
	rows, err := read(r.Context(), db, typ, c.SubjectFrom, since)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the ledger read failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, Response{Type: typ, Tenant: tenant, Key: c.SubjectFrom, Fields: c.ChangeFrom,
		Ref: c.Ref, Since: since, Note: note, States: FromRows(c, rows)})
}

func read(ctx context.Context, db eventlog.DB, typ, key string, since time.Time) ([]eventlog.Row, error) {
	var rows []eventlog.Row
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		rows, err = eventlog.LatestByKey(ctx, tx, typ, key, since)
		return err
	})
	return rows, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
