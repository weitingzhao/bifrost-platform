package devsession

import (
	"encoding/json"
	"testing"
)

func TestUnmarshalBdevStatusJSON(t *testing.T) {
	// Fixtures match bdev status --json contract:
	// stopped → pid null; running → numeric pid; dead pane → status "error".
	const fixture = `[
  {
    "name": "api-monitor",
    "label": "Monitor API",
    "group": "api",
    "ports": [8765],
    "status": "stopped",
    "pid": null,
    "uptime_sec": 0,
    "health_ok": null,
    "restarts": 0
  },
  {
    "name": "frontend",
    "label": "Trade Frontend",
    "group": "ui",
    "ports": [5173],
    "status": "running",
    "pid": 12345,
    "uptime_sec": 90,
    "health_ok": true,
    "restarts": 0
  },
  {
    "name": "daemon",
    "label": "Daemon",
    "group": "worker",
    "ports": [],
    "status": "error",
    "pid": null,
    "uptime_sec": 0,
    "health_ok": null,
    "restarts": 0
  }
]`

	var sessions []DevSession
	if err := json.Unmarshal([]byte(fixture), &sessions); err != nil {
		t.Fatalf("unmarshal bdev status JSON: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(sessions))
	}

	stopped := sessions[0]
	if stopped.Name != "api-monitor" || stopped.Status != "stopped" {
		t.Errorf("stopped session: %+v", stopped)
	}
	if stopped.PID != 0 {
		t.Errorf("stopped PID: got %d, want 0 (null)", stopped.PID)
	}
	if stopped.HealthOK != nil {
		t.Errorf("stopped health_ok: got %v, want nil", stopped.HealthOK)
	}

	running := sessions[1]
	if running.Status != "running" || running.PID != 12345 {
		t.Errorf("running session: %+v", running)
	}
	if running.UptimeSec != 90 {
		t.Errorf("running uptime_sec: got %d, want 90", running.UptimeSec)
	}
	if running.HealthOK == nil || !*running.HealthOK {
		t.Errorf("running health_ok: got %v, want true", running.HealthOK)
	}

	errored := sessions[2]
	if errored.Status != "error" || errored.PID != 0 {
		t.Errorf("error session: %+v", errored)
	}
}

func TestUnmarshalRejectsStringPID(t *testing.T) {
	// Regression guard: quoted pid must fail (old bdev contract).
	const bad = `[{"name":"x","label":"X","group":"g","ports":[1],"status":"stopped","pid":"-","uptime_sec":0,"health_ok":null}]`
	var sessions []DevSession
	if err := json.Unmarshal([]byte(bad), &sessions); err == nil {
		t.Fatal("expected unmarshal error for string pid \"-\", got nil")
	}
}
