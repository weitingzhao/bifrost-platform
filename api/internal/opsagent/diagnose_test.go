package opsagent

import "testing"

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
