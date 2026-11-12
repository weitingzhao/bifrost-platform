package agentrtapi_test

import (
	"testing"
	"time"
)

func TestExternalWorkRoutesAndPlanView(t *testing.T) {
	t.Parallel()
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

	// An event without seq gets the next one, said in the answer; a heartbeat
	// may name commits, and GET /delivery answers for one before submitted.
	events := "external-work/" + id + "/events"
	commit := "4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e"
	if code, _ = call(t, s, "viewer", "GET", "delivery?commit="+commit, nil); code != 404 {
		t.Fatalf("a commit no one names %d", code)
	}
	for _, bad := range []map[string]any{
		{"idempotency_key": "bad-sha", "type": "heartbeat", "commit_shas": []string{"4E4E4E4"}},
		{"idempotency_key": "bad-seq", "seq": -1, "type": "heartbeat"},
		{"idempotency_key": "bad-kind", "type": "linked", "root": "R2", "plan_node_id": "R2:1-1", "commit_shas": []string{commit}},
	} {
		if code, out = call(t, s, "operator", "POST", events, bad, session, "external"); code != 422 || out["error"] != "invalid" {
			t.Fatalf("%v: %d %v", bad["idempotency_key"], code, out)
		}
	}
	pushed := map[string]any{"idempotency_key": "pushed", "type": "heartbeat", "commit_shas": []string{commit}}
	code, out = call(t, s, "operator", "POST", events, pushed, session, "external")
	if code != 200 || out["seq"] != float64(2) || out["state"] != "Running" {
		t.Fatalf("heartbeat without seq %d %v", code, out)
	}
	if code, again := call(t, s, "operator", "POST", events, pushed, session, "external"); code != 200 || again["seq"] != float64(2) || again["event_id"] != out["event_id"] {
		t.Fatalf("its replay %d %v; first %v", code, again, out)
	}
	if code, out = call(t, s, "viewer", "GET", "delivery?commit="+commit[:10], nil); code != 200 || out["commit"] != commit || out["external_work_id"] != id || out["plan_node_id"] != "R2:1-1" {
		t.Fatalf("delivery of a running work's commit %d %v", code, out)
	}
	if code, out = call(t, s, "operator", "POST", events, map[string]any{"idempotency_key": "end", "seq": 0, "type": "submitted"}, session, "external"); code != 200 || out["seq"] != float64(3) || out["state"] != "Submitted" {
		t.Fatalf("submitted with seq 0 %d %v", code, out)
	}
	code, out = call(t, s, "viewer", "GET", "plan-view?root=R2", nil)
	works, _ := out["external_work"].([]any)
	if code != 200 || len(works) != 1 {
		t.Fatalf("snapshot %d %v", code, out["external_work"])
	}
	if shas, _ := works[0].(map[string]any)["commit_shas"].([]any); len(shas) != 1 || shas[0] != commit {
		t.Fatalf("the work's commits in the plan view: %v", works[0])
	}
}

// R5:5.5-19 §5, §8: the explanation is opt-in. Without explain=1 the
// response has no explain block; with it, the same response plus the
// block, pending at first and computed in the background.
func TestPlanViewExplainIsOptIn(t *testing.T) {
	t.Parallel()
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
