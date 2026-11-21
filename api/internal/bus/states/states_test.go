package states

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// 2026-11-20: the Owner asked that every module's state reach the bus and
// that a joining module mean no code change. Each state type registered with
// subject_from and change_from must be readable here with no Go of its own.
func TestEveryRegisteredStateTypeQualifies(t *testing.T) {
	tenants, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for name, tn := range tenants {
		for typ, c := range tn.Channels {
			if c.SubjectFrom == "" {
				continue
			}
			n++
			if _, err := Qualifies(tenants, name, typ); err != nil {
				t.Errorf("%s/%s: %v", name, typ, err)
			}
		}
	}
	// Positive control: node and workload state are registered today.
	if n < 2 {
		t.Fatalf("only %d state types in the register; node and workload are expected", n)
	}
	if _, err := Qualifies(tenants, "rocket", "seat.mail.arrived"); err == nil {
		t.Fatal("seat.mail.arrived has no subject_from and must not qualify")
	}
}

func TestWindowIsRetentionCapped(t *testing.T) {
	for in, want := range map[string]time.Duration{"7d": 7 * 24 * time.Hour, "12h": 12 * time.Hour, "90d": MaxWindow, "": MaxWindow, "x": MaxWindow} {
		if got := Window(in); got != want {
			t.Errorf("Window(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFromRowsKeepsTheChangeFields(t *testing.T) {
	c := registry.Channel{SubjectFrom: "workload", ChangeFrom: []string{"ready"}}
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	got := FromRows(c, []eventlog.Row{
		{ID: 7, At: at, Body: map[string]any{"workload": "data/nats", "ready": "ready"}},
		{ID: 5, At: at, Body: map[string]any{"workload": "bifrost-platform-prod/platform-api", "ready": "notready"}},
		{ID: 9, At: at, Body: map[string]any{"ready": "ready"}},
	})
	if len(got) != 2 || got[0].Subject != "bifrost-platform-prod/platform-api" || got[0].State["ready"] != "notready" || got[1].Event != "ev-7" {
		t.Fatalf("got %+v", got)
	}
}

func TestHandleRefusesWhatIsNotAStateType(t *testing.T) {
	d := Deps{DB: func() eventlog.DB { return nil }}
	for q, want := range map[string]int{"": 400, "type=seat.mail.arrived": 400, "type=no.such.type": 400, "type=cluster.workload.changed": 503} {
		rec := httptest.NewRecorder()
		d.Handle(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bus/states?"+q, nil))
		if rec.Code != want {
			t.Errorf("%q: %d %s, want %d", q, rec.Code, strings.TrimSpace(rec.Body.String()), want)
		}
	}
}

// Origin: 2026-11-21, 392 workloads in the readout for 82 in the cluster:
// an object said gone (registry.Gone) is not a state and is left out.
func TestFromRowsLeavesOutWhatIsGone(t *testing.T) {
	c := registry.Channel{SubjectFrom: "workload", ChangeFrom: []string{"ready"}}
	rows := []eventlog.Row{
		{Body: map[string]any{"workload": "cicd/affinity-assistant-1", "ready": registry.Gone}},
		{Body: map[string]any{"workload": "cicd/nats", "ready": "ready"}},
	}
	got := FromRows(c, rows)
	if len(got) != 1 || got[0].Subject != "cicd/nats" {
		t.Fatalf("states %+v, want only cicd/nats", got)
	}
}
