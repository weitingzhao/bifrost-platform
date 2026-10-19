package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TD-382: a report with a local path in it is refused (422) as before, and
// the refusal is now recorded on the host's stream with the rule that
// refused it; the attempt's end says its result did not land and that its
// host had a report refused meanwhile, and the executor is not leased the
// task again at once (TD-381).
func TestRefusedReportEndsTheAttempt(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	claimBody := map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}}
	status, claim := call(t, s, "agentd:h1", "POST", "claim", claimBody)
	if status != http.StatusOK || claim["task_id"] != task {
		t.Fatalf("claim: %d %v", status, claim)
	}
	attempt := claim["attempt_id"].(string)
	fence := func(extra map[string]any) map[string]any {
		b := map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"]}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	for _, step := range []struct {
		action string
		body   map[string]any
	}{
		{"start", fence(map[string]any{"model_provider": "cursor-mix"})},
		{"computed", fence(map[string]any{"exit_code": 0, "end_how": "exit", "conclusion": "done"})},
	} {
		if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/"+step.action, step.body); code != http.StatusOK {
			t.Fatalf("%s: %d %v", step.action, code, out)
		}
	}
	leaky := []byte("VERDICT: PASS\nthe pattern matches /home/ and /tmp/ prefixes\n")
	code, out := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(leaky), leaky, "X-Artifact-Kind", "report")
	if code != http.StatusUnprocessableEntity || out["error"] != "local_path" {
		t.Fatalf("a report with a local path: %d %v", code, out)
	}
	var rule, source, stream string
	var onAttempt bool
	must(t, pool.QueryRow(ctx, `SELECT body_json->>'rule', body_json->>'source', event_stream_id, attempt_id IS NOT NULL FROM agentrt.event
	  WHERE event_type = 'policy_alert' AND body_json->>'artifact_id' = $1`, artifactID(leaky)).Scan(&rule, &source, &stream, &onAttempt))
	if rule != "local_path" || source != "report" || stream != "host:h1" || onAttempt {
		t.Fatalf("the refusal is recorded as %s / %s on %s (on an attempt: %v); want local_path / report on host:h1", rule, source, stream, onAttempt)
	}
	var bodies int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'policy_alert' AND body_json::text LIKE '%/home/%'`).Scan(&bodies))
	if bodies != 0 {
		t.Fatal("the alert carries the path it is about")
	}
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/persist", fence(map[string]any{})); code != http.StatusConflict {
		t.Fatalf("a result without a report: %d %v", code, out)
	}
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/ended",
		map[string]any{"host_id": "h1", "end_how": "exit", "group_cleared": true}); code != http.StatusOK || out["state"] != "failed" {
		t.Fatalf("ended: %d %v", code, out)
	}
	var reason string
	must(t, pool.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'ready' ORDER BY event_id DESC LIMIT 1`, task).Scan(&reason))
	if !strings.Contains(reason, "its result was computed but not persisted; meanwhile its host had a report refused by rule local_path") {
		t.Fatalf("the task's move says %q", reason)
	}
	if code, out := call(t, s, "agentd:h1", "POST", "claim", claimBody); code != http.StatusNoContent {
		t.Fatalf("the executor was leased the task again at once: %d %v", code, out)
	}
}
