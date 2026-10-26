package agentrtapi_test

import (
	"testing"
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
