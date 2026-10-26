package agentrtapi_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var externalSpec = []byte(`{"v":1,"kind":"agentrt.external_work/v1","task":"reporter dispatch","executor":"codex","provider":"openai","repo":"platform","branch":"test","acceptance_refs":["PV1"],"estimate_minutes":[1,2]}`)

func TestReporterExternalWork(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	upload := "artifacts/" + artifactID(externalSpec)
	if code, out := call(t, s, "reporter", "PUT", upload, externalSpec, "X-Artifact-Kind", "other", session, "reporter-run"); code != 200 {
		t.Fatalf("description: %d %v", code, out)
	}
	reg := map[string]any{"idempotency_key": "reg", "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "reporter-run", "artifact_id": artifactID(externalSpec)}
	code, out := call(t, s, "reporter", "POST", "external-work", reg, session, "reporter-run")
	if code != 200 {
		t.Fatalf("register: %d %v", code, out)
	}
	id := out["external_work_id"].(string)
	if code, replay := call(t, s, "reporter", "POST", "external-work", reg, session, "reporter-run"); code != 200 || replay["external_work_id"] != id {
		t.Fatalf("registration replay: %d %v", code, replay)
	}
	eventPath := "external-work/" + id + "/events"
	event := map[string]any{"idempotency_key": "start", "seq": 1, "type": "started"}
	for _, caller := range []string{"reporter", "operator"} {
		as := "another-session"
		if caller == "operator" {
			as = "reporter-run" // Another token cannot impersonate the same session.
		}
		if code, out := call(t, s, caller, "POST", eventPath, event, session, as); code != 403 {
			t.Fatalf("foreign writer %s: %d %v", caller, code, out)
		}
	}
	for _, typ := range []string{"verified", "verification_failed"} {
		bad := map[string]any{"idempotency_key": typ, "seq": 1, "type": typ}
		if code, out := call(t, s, "reporter", "POST", eventPath, bad, session, "reporter-run"); code != 422 {
			t.Fatalf("verification %s: %d %v", typ, code, out)
		}
	}
	for _, caller := range []string{"", "viewer", "agentd:h1"} {
		for _, path := range []string{"external-work", eventPath} {
			if code, out := call(t, s, caller, "POST", path, reg, session, "reporter-run"); code != http.StatusUnauthorized {
				t.Fatalf("unauthorized %s %s: %d %v", caller, path, code, out)
			}
		}
	}
	if code, out := call(t, s, "", "PUT", upload, externalSpec, "X-Artifact-Kind", "other", session, "reporter-run"); code != 401 {
		t.Fatalf("anonymous upload: %d %v", code, out)
	}
	for i, typ := range []string{"started", "heartbeat", "submitted"} {
		event = map[string]any{"idempotency_key": typ, "seq": i + 1, "type": typ}
		if code, out := call(t, s, "reporter", "POST", eventPath, event, session, "reporter-run"); code != 200 {
			t.Fatalf("own %s: %d %v", typ, code, out)
		}
		if code, out := call(t, s, "reporter", "POST", eventPath, event, session, "reporter-run"); code != 200 {
			t.Fatalf("event replay %s: %d %v", typ, code, out)
		}
		if code, out := call(t, s, "operator", "POST", eventPath, event, session, "reporter-run"); code != 403 {
			t.Fatalf("another token replaying %s: %d %v", typ, code, out)
		}
	}
	// A record registered by a proposer also remains private to its actor.
	code, out = call(t, s, "operator", "POST", "external-work", reg, session, "proposer-run")
	if code != 200 {
		t.Fatalf("proposer register: %d %v", code, out)
	}
	path := "external-work/" + out["external_work_id"].(string) + "/events"
	for _, as := range []string{"reporter-run", "proposer-run"} {
		if code, out := call(t, s, "reporter", "POST", path, event, session, as); code != 403 {
			t.Fatalf("proposer's record as %s: %d %v", as, code, out)
		}
	}
}

func TestReporterUploadScope(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	var before int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.artifact`).Scan(&before))
	for kind := range store.ArtifactKinds {
		if kind == "other" {
			continue
		}
		t.Run(kind, func(t *testing.T) {
			code, out := call(t, s, "reporter", "PUT", "artifacts/"+artifactID(externalSpec), externalSpec, "X-Artifact-Kind", kind, session, "limited")
			if code != 403 {
				t.Fatalf("other artifact kind: %d %v", code, out)
			}
		})
	}
	for i, body := range []string{
		`{"v":1,"kind":"something_else"}`, `plain text`,
		`{"v":1,"kind":"agentrt.external_work/v1"}`,
		string(externalSpec[:len(externalSpec)-1]) + `,"extra":"payload"}`,
		string(externalSpec[:len(externalSpec)-1]) + `,"kind":"something_else"}`,
		string(externalSpec[:len(externalSpec)-1]) + `,"Kind":"something_else","extra":"payload"}`,
	} {
		t.Run(fmt.Sprintf("other-body-%d", i), func(t *testing.T) {
			raw := []byte(body)
			if code, out := call(t, s, "reporter", "PUT", "artifacts/"+artifactID(raw), raw, "X-Artifact-Kind", "other", session, "limited"); code != 403 {
				t.Fatalf("non-description: %d %v", code, out)
			}
		})
	}
	var after int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.artifact`).Scan(&after))
	if after != before {
		t.Fatalf("refused uploads wrote artifacts: %d -> %d", before, after)
	}
}

func TestReporterCannotPropose(t *testing.T) {
	// Refuse at the route, before even consulting the database or body.
	s := server(t, nil)
	for _, path := range []string{"missions", "missions/M-1/tasks", "goals", "plan/proposals", "plan/imports", "plan/R2/notes", "messages", "messages/ack", "rules/routing:dev/proposals", "rules/routing:dev/sign"} {
		t.Run(path, func(t *testing.T) {
			for _, as := range []string{"limited", "seat:chief_of_staff"} {
				if code, out := call(t, s, "reporter", "POST", path, map[string]any{}, session, as); code != 403 {
					t.Fatalf("reporter proposal as %s: %d %v", as, code, out)
				}
			}
			for _, caller := range []string{"operator", "admin", "seat:chief_of_staff"} {
				if code, out := call(t, s, caller, "POST", path, map[string]any{}, session, "proposer"); code != 503 {
					t.Fatalf("existing proposer %s: %d %v", caller, code, out)
				}
			}
		})
	}
}
