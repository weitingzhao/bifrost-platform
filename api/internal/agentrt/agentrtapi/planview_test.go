package agentrtapi_test

import (
	"testing"
	"time"
)

func TestExternalWorkRoutesAndPlanView(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	spec := []byte(`{"v":1,"kind":"agentrt.external_work/v1","task":"测试","executor":"codex","provider":"openai","repo":"platform","branch":"test","acceptance_refs":["PV1"],"estimate_minutes":[1,2]}`)
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "other", session, "external"); code != 200 {
		t.Fatalf("upload %d %v", code, out)
	}
	body := map[string]any{"idempotency_key": "reg", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "run", "artifact_id": artifactID(spec)}
	for _, role := range []string{"viewer", "agentd:h1", ""} {
		code, _ := call(t, s, role, "POST", "external-work", body, session, "external")
		if code != 401 && code != 403 {
			t.Fatalf("%s write %d", role, code)
		}
	}
	code, out := call(t, s, "operator", "POST", "external-work", body, session, "external")
	if code != 200 {
		t.Fatalf("register %d %v", code, out)
	}
	id := out["external_work_id"].(string)
	body["source_run_key"] = "different"
	if code, _ = call(t, s, "operator", "POST", "external-work", body, session, "external"); code != 409 {
		t.Fatalf("conflict %d", code)
	}
	event := map[string]any{"idempotency_key": "start", "seq": 1, "type": "started"}
	if code, _ = call(t, s, "operator", "POST", "external-work/"+id+"/events", event, session, "intruder"); code != 403 {
		t.Fatalf("not yours %d", code)
	}
	if code, out = call(t, s, "operator", "POST", "external-work/"+id+"/events", event, session, "external"); code != 200 {
		t.Fatalf("start %d %v", code, out)
	}
	event["seq"], event["type"], event["idempotency_key"] = 2, "verified", "forged"
	if code, _ = call(t, s, "operator", "POST", "external-work/"+id+"/events", event, session, "external"); code != 422 {
		t.Fatalf("verification %d", code)
	}
	if code, out = call(t, s, "viewer", "GET", "plan-view?root=R2", nil); code != 200 || out["schema"] != "agentrt.plan_view_response/v1" {
		t.Fatalf("snapshot %d %v", code, out)
	}
	if code, _ = call(t, s, "viewer", "GET", "plan-view?root=missing", nil); code != 404 {
		t.Fatalf("missing %d", code)
	}
	if code, out = call(t, s, "viewer", "GET", "external-work/"+id, nil); code != 200 || out["seq"] != float64(1) {
		t.Fatalf("resume %d %v", code, out)
	}
}

// R5:5.5-19 §5, §8: the explanation is opt-in. Without explain=1 the
// response has no explain block; with it, the same response plus the
// block, pending at first and computed in the background.
func TestPlanViewExplainIsOptIn(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	code, plain := call(t, s, "viewer", "GET", "plan-view?root=R2", nil)
	if code != 200 || plain["explain"] != nil {
		t.Fatalf("without explain=1: %d, explain %v", code, plain["explain"])
	}
	code, out := call(t, s, "viewer", "GET", "plan-view?root=R2&explain=1", nil)
	ex, _ := out["explain"].(map[string]any)
	if code != 200 || ex == nil || ex["status"] != "pending" {
		t.Fatalf("with explain=1: %d, explain %v", code, out["explain"])
	}
	for k := range plain {
		if _, ok := out[k]; !ok {
			t.Fatalf("explain=1 dropped %s", k)
		}
	}
	if len(out) != len(plain)+1 {
		t.Fatalf("explain=1 added more than the block: %d keys, %d without", len(out), len(plain))
	}
	deadline := time.Now().Add(10 * time.Second)
	for ex["status"] == "pending" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		_, out = call(t, s, "viewer", "GET", "plan-view?root=R2&explain=1", nil)
		ex, _ = out["explain"].(map[string]any)
	}
	if ex["status"] != "ok" || ex["read_at"] == nil || ex["steps"] == nil {
		t.Fatalf("computed: %v", ex)
	}
	if code, _ = call(t, s, "viewer", "GET", "plan-view?root=missing&explain=1", nil); code != 404 {
		t.Fatalf("missing root with explain=1: %d", code)
	}
}
