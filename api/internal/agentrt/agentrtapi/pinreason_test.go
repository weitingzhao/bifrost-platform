package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7-8 slice 1: an implement task pinned to the coordinator says why
// (pin_reason, in its task_proposed body); AGENTRT_PIN_REASON warn accepts
// one without and counts it, refuse answers 422; GET /pin-share counts the
// last 7 days.
func TestPinReasonOverHTTP(t *testing.T) {
	t.Parallel()
	pool := pinWorld(t)
	warn := server(t, pool)
	refuse := server(t, pool, func(a *agentrtapi.API) { a.PinReason = store.PinReasonRefuse })
	spec := implSpec(t, "# implement y\n")
	if code, out := call(t, warn, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec", session, "s1"); code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, out)
	}
	task := func(key, reason string, pinned bool) map[string]any {
		tk := map[string]any{"ref": "a", "plan_node_id": "R2:1-1", "role": "implement", "task_class": "go", "title": "a",
			"spec_artifact_id": artifactID(spec), "effect_class": "branch", "repo": "bifrost-platform", "base_commit": commitA}
		if pinned {
			tk["executor_pin"], tk["providers"] = rules.PinCoordinator, []string{"anthropic"}
		}
		if reason != "" {
			tk["pin_reason"] = reason
		}
		return map[string]any{"idempotency_key": key, "tasks": []any{tk}}
	}
	event := func(out map[string]any) map[string]any {
		t.Helper()
		id := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
		var ev map[string]any
		must(t, pool.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_proposed'`, id).Scan(&ev))
		return ev
	}
	counts := func() (tasks, events int) {
		must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.task), (SELECT count(*) FROM agentrt.event)`).Scan(&tasks, &events))
		return
	}
	share := func() map[string]any {
		t.Helper()
		code, out := call(t, warn, "viewer", "GET", "pin-share", nil)
		if code != http.StatusOK {
			t.Fatalf("pin-share: %d %v", code, out)
		}
		return out
	}
	base := share()
	n := func(m map[string]any, k string) int { return int(m[k].(float64)) }

	// A pin with its reason: accepted under refuse, the reason in the event.
	const why = "the landing drill needs the coordinator's own Mac"
	code, out := call(t, refuse, "seat:coordinator", "POST", "missions/M-1/tasks", task("k-why", why, true), session, "local_coordinator")
	if code != http.StatusOK {
		t.Fatalf("a pin with a reason: %d %v", code, out)
	}
	if ev := event(out); ev["pin_reason"] != why || ev["executor_pin"] != rules.PinCoordinator {
		t.Fatalf("a pin with a reason: event %v", ev)
	}

	// A pin without one: refused under refuse, naming the field, nothing
	// written; accepted under warn, its event without a reason.
	t0, e0 := counts()
	code, out = call(t, refuse, "seat:coordinator", "POST", "missions/M-1/tasks", task("k-none", "", true), session, "local_coordinator")
	if code != http.StatusUnprocessableEntity || out["error"] != "invalid" || !strings.Contains(out["message"].(string), "pin_reason") {
		t.Fatalf("a pin without a reason under refuse: %d %v", code, out)
	}
	if t1, e1 := counts(); t1 != t0 || e1 != e0 {
		t.Fatalf("the refusal left %d tasks and %d events", t1-t0, e1-e0)
	}
	code, out = call(t, warn, "seat:coordinator", "POST", "missions/M-1/tasks", task("k-none", "", true), session, "local_coordinator")
	if code != http.StatusOK {
		t.Fatalf("a pin without a reason under warn: %d %v", code, out)
	}
	if ev := event(out); ev["executor_pin"] != rules.PinCoordinator || ev["pin_reason"] != nil {
		t.Fatalf("a pin without a reason under warn: event %v", ev)
	}

	// No pin: unchanged, under refuse too.
	code, out = call(t, refuse, "operator", "POST", "missions/M-1/tasks", task("k-plain", "", false), session, "s1")
	if code != http.StatusOK {
		t.Fatalf("no pin: %d %v", code, out)
	}
	if ev := event(out); ev["executor_pin"] != nil || ev["pin_reason"] != nil {
		t.Fatalf("no pin: event %v", ev)
	}

	// A reason too long, blank, or on a task with no pin: refused in
	// either mode, nothing written.
	t0, e0 = counts()
	for name, body := range map[string]map[string]any{
		"201 bytes":   task("k-long", strings.Repeat("x", store.MaxPinReason+1), true),
		"blank":       task("k-blank", "   ", true),
		"with no pin": task("k-unpinned", why, false),
	} {
		caller := "seat:coordinator"
		if body["tasks"].([]any)[0].(map[string]any)["executor_pin"] == nil {
			caller = "operator"
		}
		if code, out := call(t, warn, caller, "POST", "missions/M-1/tasks", body, session, "local_coordinator"); code != http.StatusUnprocessableEntity ||
			!strings.Contains(out["message"].(string), "pin_reason") {
			t.Errorf("a reason %s: %d %v", name, code, out)
		}
	}
	if t1, e1 := counts(); t1 != t0 || e1 != e0 {
		t.Fatalf("the refusals left %d tasks and %d events", t1-t0, e1-e0)
	}
	// Exactly 200 bytes is a reason.
	if code, out := call(t, warn, "seat:coordinator", "POST", "missions/M-1/tasks", task("k-200", strings.Repeat("y", store.MaxPinReason), true),
		session, "local_coordinator"); code != http.StatusOK {
		t.Fatalf("a 200-byte reason: %d %v", code, out)
	}

	// The share: three more implement tasks, two pinned (with a reason),
	// one pinned without, one not pinned.
	got := share()
	if n(got, "implement")-n(base, "implement") != 4 || n(got, "pinned")-n(base, "pinned") != 3 || n(got, "unexplained")-n(base, "unexplained") != 1 ||
		n(got, "days") != 7 {
		t.Fatalf("share: before %v, after %v", base, got)
	}
	want := "Implement tasks pinned to the coordinator, last 7 days: "
	if s, _ := got["summary"].(string); !strings.HasPrefix(s, want) || !strings.HasSuffix(s, " without a reason)") {
		t.Fatalf("share summary: %q", s)
	}
	if code, _ := call(t, warn, "", "GET", "pin-share", nil); code != http.StatusUnauthorized {
		t.Fatalf("an anonymous share read: %d", code)
	}
}
