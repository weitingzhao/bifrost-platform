//bifrost:owner foundation

package store

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Each checked field takes a value of its shape and refuses another, as the
// request's error (review 145 B6).
func TestEventFieldValues(t *testing.T) {
	t.Parallel()
	const ulid = "01M59KGZ56VX9YCCCN9KCR9264"
	const sha = "sha256:05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"
	covered := map[string]bool{}
	// The checked fields are the declared sum: this package's tables and the
	// coordination domain's (package coord).
	checked := eventlog.CheckedFields()
	for _, c := range []struct{ event, field, good, bad string }{
		{"pipeline_seen", "tree", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bad value"},
		{"pipeline_seen", "tested_by", "ci-original", "bad value"},
		{"metrics_computed", "source", "computed", "manual"},
		{"metrics_computed", "label", "baseline:before", "before"},
		{"metrics_computed", "artifact_id", sha, "bad"},
		{"plan_node_created", "node_kind", "step", "leaf"},
		{"plan_node_state_changed", "to", "dropped", "deleted"},
		{"plan_node_state_changed", "from", "planned", "draft"},
		{"plan_change_decided", "decision", "expired", "maybe"},
		{"mission_state_changed", "from", "awaiting_owner", "waiting"},
		{"mission_state_changed", "to", "expired", "closed"},
		{"artifact_put", "artifact_id", sha, "sha1:abc"},
		{"artifact_put", "artifact_kind", "checkpoint_patch", "patch"},
		{"message_posted", "message_id", "m_" + ulid, "m_" + ulid + "0"},
		{"message_posted", "kind", "wake", "ping"},
		{"message_acked", "message_id", "m_" + ulid, "message"},
		{"legacy_imported", "digest", sha, "ace660f699888d88caf667e3eef6cedec535df23003c0d1aea5466bc68ff9ba4"},
		{"control_set", "scope", "host", "region"},
		{"control_set", "kind", "drain", "kill"},
		{"rule_proposed", "direction", "loosen", "relax"},
		{"rule_proposed", "text_artifact_id", sha, "rule.yaml"},
		{"rule_approved", "approval_id", "appr_0123456789abcdef", "#212"},
		{"rule_approved", "text_artifact_id", sha, "rule.yaml"},
		{"task_proposed", "role", "brief", "author"},
		{"task_proposed", "executor_pin", "external:coordinator", "external:other"},
		{"controller_loop_failing", "line_digest", "sha256:" + strings.Repeat("a", 64), "agentrt-leases"},
		{"controller_loop_recovered", "line_digest", "sha256:" + strings.Repeat("b", 64), "sha256:short"},
		{"artifact_refused", "rule", "workspace_ref", "secret"},
		{"artifact_refused", "kind", "verify", "spec"},
		{"policy_alert", "rule", "approval_claim", "local_path"},
		{"policy_alert", "source", "plan_note", "review"},
		{"policy_alert", "phrase", "Owner 已批准", "POLICY_ALERT"},
		{"policy_alert", "message_id", "m_" + ulid, "M-12"},
		{"approval_seen", "created_at", "2026-01-01T00:00:00Z", "yesterday"},
		{"approval_seen", "decided_at", "2026-01-01T00:00:00Z", "yesterday"},
		{"question_seen", "created_at", "2026-01-01T00:00:00Z", "yesterday"},
		{"question_seen", "withdrawn_at", "2026-01-01T00:00:00Z", "yesterday"},
		{"question_seen", "expired_at", "2026-01-01T00:00:00Z", "yesterday"},
		{"pipeline_seen", "created_at", "2026-01-01T00:00:00Z", "yesterday"},
		{"pipeline_seen", "started_at", "2026-01-01T00:00:00Z", "yesterday"},
		{"pipeline_seen", "completed_at", "2026-01-01T00:00:00Z", "yesterday"},
		{"pipeline_seen", "kind", "ci", "build"},
		{"pipeline_seen", "result", "Succeeded", "success"},
		{"pipeline_seen", "reason", "Completed", "two words"},
		{"question_seen", "answered_at", "2026-10-19T11:40:00Z", "now"},
		{"owner_wait_closed", "outcome", "dropped", "skipped"},
		{"ci_seen", "commit", "42bb2c9672b94b1178797d062df0e72dd789f200", "42bb2c9"},
		{"deploy_seen", "env", "prod", "production"},
		{"deploy_seen", "commit", "42bb2c9672b94b1178797d062df0e72dd789f200", "main"},
	} {
		covered[c.event+"."+c.field] = true
		if why := badValue(c.event, c.field, c.good); why != "" {
			t.Errorf("%s.%s refused %q: %s", c.event, c.field, c.good, why)
		}
		if badValue(c.event, c.field, c.bad) == "" {
			t.Errorf("%s.%s took %q", c.event, c.field, c.bad)
		}
		if !slices.Contains(checked[c.event], c.field) {
			t.Errorf("%s.%s is not checked", c.event, c.field)
		}
	}
	// Every checked field has its pair above (review 334 B3) and is in its
	// event's body, so no check sits on a field no event has.
	for event, fields := range checked {
		// Bus types (dotted names) are checked by their publisher's tests
		// (evidence's for ci.verdict.changed); see TestEventTypesAreTheDatabases.
		if strings.Contains(event, ".") {
			continue
		}
		for _, name := range fields {
			if !covered[event+"."+name] {
				t.Errorf("%s.%s is checked but has no good and bad value here", event, name)
			}
			found := false
			body, _ := eventlog.BodyOf(event)
			for _, f := range body {
				found = found || f.Name == name
			}
			if !found {
				t.Errorf("%s.%s is checked but not in the body", event, name)
			}
		}
	}
	// Through checkBody: refused as the request's error, after the kind.
	err := eventlog.CheckBody("mission_state_changed", map[string]any{"from": "proposed", "to": "stuck"})
	if !errors.Is(err, eventlog.ErrInvalid) || !strings.Contains(err.Error(), `field "to" is "stuck"`) {
		t.Fatalf("a bad state: %v", err)
	}
	if err = eventlog.CheckBody("mission_state_changed", map[string]any{"from": "proposed", "to": "active"}); err != nil {
		t.Fatalf("a good state: %v", err)
	}
}

// Values the PROD, STG and DEV runtimes had written by 2026-10-19 11:5x pass:
// every distinct value of a checked field there (622, read with agent_reader
// from bifrost_platform, bifrost_platform_stg and bifrost_platform_dev) was
// run through badValue once and none was refused; these are a sample of
// them, so a check made stricter later meets what the runtime writes.
func TestEventFieldValuesWrittenOnline(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		event, field string
		values       []string
	}{
		{"artifact_put", "artifact_kind", []string{"checkpoint_patch", "log_tail", "report", "review", "task_spec", "verify"}},
		{"control_set", "kind", []string{"stop"}},
		{"control_set", "scope", []string{"mission", "task"}},
		{"message_acked", "message_id", []string{"m_01M57ZRW6FVCJ027DSNMQYW98G", "m_01M5A3Z1V3WVNEJ6CKBCFQXSHD"}},
		{"message_posted", "kind", []string{"answer", "ask", "note"}},
		{"mission_state_changed", "from", []string{"active", "proposed"}},
		{"mission_state_changed", "to", []string{"active", "done"}},
		{"task_proposed", "role", []string{"brief", "implement", "plan", "review", "verify"}},
	} {
		for _, v := range c.values {
			if why := badValue(c.event, c.field, v); why != "" {
				t.Errorf("%s.%s refuses %q, written online: %s", c.event, c.field, v, why)
			}
		}
	}
}

// An integer field takes integers that fit in an int64 and nothing that only
// looks like one: a float, a whole float, a number past int64, a digit string
// (review 145 B6: the store's side of canon's int64 subset).
func TestEventIntegersOnly(t *testing.T) {
	t.Parallel()
	// The kind itself is the event log's (eventlog TestOfKindIntegersOnly);
	// here, through a body check.
	if err := eventlog.CheckBody("artifact_put", map[string]any{"artifact_id": "sha256:" + strings.Repeat("a", 64), "artifact_kind": "other", "size_bytes": 2.0, "uploaded_by": "thread:s1"}); err == nil || !strings.Contains(err.Error(), "wrong kind") {
		t.Fatalf("a whole float: %v", err)
	}
}

func TestMetricBodiesRejectInvalid(t *testing.T) {
	t.Parallel()
	for _, defs := range []any{nil, "definitions", []any{map[string]any{"id": "Bad", "version": 1, "sha256": strings.Repeat("a", 64)}}, []any{map[string]any{"id": "lead_time", "version": 0, "sha256": strings.Repeat("a", 64)}}, []any{map[string]any{"id": "lead_time", "version": 1, "sha256": "bad"}}} {
		if err := eventlog.CheckBody("metrics_computed", map[string]any{"source": "computed", "rows": 0, "definitions": defs}); err == nil {
			t.Errorf("accepted definitions %#v", defs)
		}
	}
	for _, typ := range []string{"ci_red_classified"} {
		if err := eventlog.CheckBody(typ, map[string]any{}); err == nil {
			t.Errorf("%s has a body before wave M-b", typ)
		}
	}
	if err := eventlog.CheckBody("metrics_computed", map[string]any{"source": "imported", "rows": 0, "definitions": []any{map[string]any{"id": "lead_time", "version": 1, "sha256": strings.Repeat("a", 64)}}}); err != nil {
		t.Fatal(err)
	}
}
