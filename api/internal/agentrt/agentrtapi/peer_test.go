package agentrtapi_test

import (
	"net/http"
	"testing"
)

// T03 同伴越权 (v5 §5.3): another host's agentd token reaches nothing of an
// attempt it does not hold, on every attempt route: naming the holder's host
// it is the wrong host (403), naming its own it is fenced (409). A token
// that is not an agentd's is refused the routes (401: no such ability). The
// attempt is as it was, and its holder carries on.
func TestPeerReachesNoOtherAttempt(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	status, claim := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}})
	if status != http.StatusOK {
		t.Fatalf("claim: %d %v", status, claim)
	}
	attempt, epoch := claim["attempt_id"].(string), claim["lease_epoch"]
	own := func(extra map[string]any) map[string]any {
		b := map[string]any{"host_id": "h1", "lease_epoch": epoch}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/start", own(map[string]any{"model_provider": "cursor-mix"})); code != http.StatusOK {
		t.Fatalf("start: %d %v", code, out)
	}
	report := []byte("# done\n")
	if code, out := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(report), report, "X-Artifact-Kind", "report"); code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, out)
	}
	var before int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE attempt_id = $1`, attempt).Scan(&before))

	result := map[string]any{"report": artifactID(report), "pushed_ref": claim["pushed_ref"], "pushed_commit": pushedAt}
	for action, extra := range map[string]map[string]any{
		"heartbeat":  nil,
		"start":      {"model_provider": "cursor-mix"},
		"checkpoint": {"checkpoint_seq": 1, "artifacts": []string{artifactID(report)}},
		"computed":   {"exit_code": 0, "end_how": "exit", "conclusion": "done"},
		"persist":    result,
		"submit":     nil,
		"salvage":    result,
		"ended":      {"end_how": "exit", "group_cleared": true},
	} {
		for _, c := range []struct {
			host string
			code int
			err  string
		}{{"h1", http.StatusForbidden, "wrong_host"}, {"h2", http.StatusConflict, "fenced"}} {
			body := map[string]any{"host_id": c.host, "lease_epoch": epoch}
			for k, v := range extra {
				body[k] = v
			}
			if code, out := call(t, s, "agentd:h2", "POST", "attempts/"+attempt+"/"+action, body); code != c.code || out["error"] != c.err {
				t.Errorf("h2's token on %s naming %s: %d %v; want %d %s", action, c.host, code, out, c.code, c.err)
			}
		}
		for _, who := range []string{"reporter", "operator", "seat:chief_of_staff", "viewer"} {
			if code, _ := call(t, s, who, "POST", "attempts/"+attempt+"/"+action, own(extra)); code != http.StatusUnauthorized {
				t.Errorf("%s on %s: %d", who, action, code)
			}
		}
	}
	var after int
	var state string
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE attempt_id = $1`, attempt).Scan(&after))
	must(t, pool.QueryRow(ctx, `SELECT state FROM agentrt.attempt WHERE attempt_id = $1`, attempt).Scan(&state))
	if after != before || state != "running" {
		t.Fatalf("the peers wrote %d events; the attempt is %s", after-before, state)
	}
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/heartbeat", own(nil)); code != http.StatusOK {
		t.Fatalf("the holder's heartbeat: %d %v", code, out)
	}
}
