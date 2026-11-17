package opsagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiagnoseCrashLoop(t *testing.T) {
	payload := alertmanagerPayload{
		Status:   "firing",
		Receiver: "ops-agent",
		Alerts: []alertmanagerAlert{{
			Labels: map[string]string{
				"alertname":  "KubePodCrashLooping",
				"namespace":  "bifrost-stg",
				"deployment": "api-monitor",
			},
		}},
	}
	d := Diagnose(payload)
	if len(d.SuggestedL1) == 0 {
		t.Fatal("expected L1 suggestion")
	}
	if d.SuggestedL1[0].Tool != "rollout_restart_deployment" {
		t.Fatalf("got %s", d.SuggestedL1[0].Tool)
	}
}

func TestDiagnoseNodeAlert(t *testing.T) {
	payload := alertmanagerPayload{
		Status:   "firing",
		Receiver: "ops-agent",
		Alerts: []alertmanagerAlert{{
			Labels: map[string]string{
				"alertname": "NodeNotReady",
				"node":      "compute-1",
			},
		}},
	}
	d := Diagnose(payload)
	if len(d.SuggestedL2) == 0 {
		t.Fatal("expected L2 suggestion")
	}
	if d.SuggestedL2[0].Tool != "drain_node" {
		t.Fatalf("got %s", d.SuggestedL2[0].Tool)
	}
}

// 2026-11-16: the webhook hands every alert to the bus, each with its own
// status (the payload's when the alert has none).
func TestHandleAlertmanagerAnnounces(t *testing.T) {
	var got []AlertFact
	h := &Handler{Announce: func(_ context.Context, a []AlertFact) error { got = a; return nil }}
	body := `{"status":"firing","receiver":"bifrost-ops-agent","alerts":[` +
		`{"labels":{"alertname":"BifrostTektonPipelineRunTTLStale","namespace":"cicd","severity":"warning"}},` +
		`{"status":"resolved","labels":{"alertname":"NodeDown"}}]}`
	rec := httptest.NewRecorder()
	h.HandleAlertmanager(rec, httptest.NewRequest(http.MethodPost, "/ops-agent/alertmanager", strings.NewReader(body)))
	want := []AlertFact{
		{Name: "BifrostTektonPipelineRunTTLStale", Namespace: "cicd", Severity: "warning", Status: "firing"},
		{Name: "NodeDown", Status: "resolved"},
	}
	if rec.Code != http.StatusOK || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("code=%d got=%+v", rec.Code, got)
	}
}
