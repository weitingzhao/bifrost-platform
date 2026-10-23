package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Each checked field takes a value of its shape and refuses another, as the
// request's error (review 145 B6).
func TestEventFieldValues(t *testing.T) {
	t.Parallel()
	const ulid = "01M59KGZ56VX9YCCCN9KCR9264"
	const sha = "sha256:05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"
	covered := map[string]bool{}
	for _, c := range []struct{ event, field, good, bad string }{
		{"plan_node_created", "node_kind", "step", "leaf"},
		{"plan_node_state_changed", "to", "dropped", "deleted"},
		{"plan_node_state_changed", "from", "planned", "draft"},
		{"plan_change_decided", "decision", "expired", "maybe"},
		{"mission_state_changed", "from", "awaiting_owner", "waiting"},
		{"mission_state_changed", "to", "expired", "closed"},
		{"task_proposed", "role", "brief", "author"},
		{"task_state_changed", "to", "blocked", "stuck"},
		{"task_state_changed", "from", "verifying", "checking"},
		{"task_dependency_added", "depends_on_task_id", "t_" + ulid, "t1"},
		{"attempt_leased", "executor", "exec:codex", "codex"},
		{"attempt_checkpointed", "artifacts", sha, "sha256:short"},
		{"attempt_stale", "lease_expires_at", "2026-10-15T21:50:18.181636Z", "2026-10-15 21:50"},
		{"attempt_renewed", "lease_expires_at", "2026-10-19T02:14:41.057074107Z", "yesterday"},
		{"attempt_computed", "end_how", "daemon_restart", "crashed"},
		{"attempt_computed", "conclusion", "none", "ok"},
		{"attempt_ended", "end_how", "lease_lost", "lost"},
		{"attempt_salvaged", "artifacts", sha, "report.md"},
		{"attempt_persisted", "artifacts", sha, "SHA256:" + strings.Repeat("A", 64)},
		{"attempt_cancelled", "accepted_attempt", "a_" + ulid, "a_" + strings.ToLower(ulid)},
		{"host_registered", "host_kind", "vm", "laptop"},
		{"host_state_changed", "to", "draining", "down"},
		{"host_state_changed", "from", "retired", "gone"},
		{"verification_recorded", "verification_id", "v_" + ulid, "v-1"},
		{"verification_recorded", "method", "gate", "manual"},
		{"verification_recorded", "verdict", "fail", "failed"},
		{"verification_recorded", "env", "stg", "staging"},
		{"artifact_put", "artifact_id", sha, "sha1:abc"},
		{"artifact_put", "artifact_kind", "checkpoint_patch", "patch"},
		{"message_posted", "message_id", "m_" + ulid, "m_" + ulid + "0"},
		{"message_posted", "kind", "wake", "ping"},
		{"message_acked", "message_id", "m_" + ulid, "message"},
		{"policy_alert", "rule", "approval_claim", "claim"},
		{"policy_alert", "rule", "seat_alert", "seat"},
		{"policy_alert", "source", "message", "log_tail"},
		{"legacy_imported", "digest", sha, "ace660f699888d88caf667e3eef6cedec535df23003c0d1aea5466bc68ff9ba4"},
		{"policy_alert", "rule", "local_path", "/home/"},
		{"policy_alert", "rule", "repeated_failure", "failed"},
		{"policy_alert", "source", "review", "report.md"},
		{"policy_alert", "phrase", "a local path (H8)", "/home/someone"},
		{"policy_alert", "source", "seat_plan", "plan"},
		{"policy_alert", "phrase", "Owner 已批准", "the text's own words"},
		{"policy_alert", "phrase", "POLICY_ALERT", "POLICY_ALERT: thread:s1 — claims"},
		{"policy_alert", "artifact_id", sha, "spec.md"},
		{"policy_alert", "message_id", "m_" + ulid, "t_" + ulid},
		{"policy_alert", "line_digest", sha, "05851c9c"},
		{"control_set", "scope", "host", "region"},
		{"control_set", "kind", "drain", "kill"},
		{"pool_cooldown_set", "until", "2026-10-24T20:40:42.000000Z", "2026-10-24"},
		{"capability_scored", "source", "benchmark", "bench"},
		{"capability_scored", "expires_at", "2026-11-14T19:24:35.169969Z", "2026-11-14T19:24:35"},
		{"capability_scored", "started_at", "2026-10-19T06:05:04.538749668Z", "Mon Oct 19"},
		{"pool_measured", "source", "exit_text", "guess"},
		{"pool_measured", "resets_at", "2026-10-19T13:00:00.000000Z", "13:00"},
		{"budget_charged", "attempt_id", "a_" + ulid, "t_" + ulid},
		{"rule_proposed", "direction", "loosen", "relax"},
		{"rule_proposed", "text_artifact_id", sha, "rule.yaml"},
		{"candidate_status_changed", "to", "probation", "paused"},
		{"candidate_status_changed", "from", "qualifying", "trial"},
		{"question_seen", "answered_at", "2026-10-19T11:40:00Z", "now"},
		{"owner_wait_closed", "outcome", "dropped", "skipped"},
		{"ci_seen", "commit", "42bb2c9672b94b1178797d062df0e72dd789f200", "42bb2c9"},
		{"deploy_seen", "env", "prod", "production"},
		{"deploy_seen", "commit", "42bb2c9672b94b1178797d062df0e72dd789f200", "main"},
		{"note", "landed", "2333e00753f91c74c8b2f4d693a803f8e89e7892", "2333e00"},
		{"note", "pruned", "refs/heads/agent/t_" + ulid + "/a2", "refs/heads/main"},
		{"note", "prune_refused", "refs/heads/agent/t_" + ulid + "/a1", "agent/t_" + ulid + "/a1"},
		{"note", "prune_failed", "refs/heads/agent/t_" + ulid + "/a3", "refs/heads/agent/t_" + ulid + "/a0"},
		{"note", "voided", "appr_0123456789abcdef", "#212"},
		{"route_decided", "fingerprint", sha, "05851c9c"},
		{"wait_limit_passed", "wait", "task_ready", "ready"},
		{"wait_limit_passed", "ref", "m_" + ulid, "M-12"},
		{"wait_limit_passed", "since", "2026-10-21T12:00:00.123456Z", "12:00"},
		{"wait_limit_passed", "basis", "history_usual", "usual"},
		{"issue_submitted", "issue_id", "BI-12", "BI-012"},
		{"issue_added", "issue_id", "BI-12", "BI-012"},
		{"issue_brief_drafted", "issue_id", "BI-12", "BI-012"},
		{"issue_asked", "issue_id", "BI-12", "BI-012"},
		{"issue_answered", "issue_id", "BI-12", "BI-012"},
		{"issue_classified", "issue_id", "BI-12", "BI-012"},
		{"issue_design_put", "issue_id", "BI-12", "BI-012"},
		{"issue_reviewed", "issue_id", "BI-12", "BI-012"},
		{"issue_owner_decided", "issue_id", "BI-12", "BI-012"},
		{"issue_priority_set", "issue_id", "BI-12", "BI-012"},
		{"issue_taken", "issue_id", "BI-12", "BI-012"},
		{"issue_replied", "issue_id", "BI-12", "BI-012"},
		{"issue_state_changed", "issue_id", "BI-12", "BI-012"},
		{"issue_merged", "issue_id", "BI-12", "BI-012"},
		{"issue_brief_drafted", "brief_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "sha256:" + "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"},
		{"issue_asked", "brief_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "sha256:" + "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"},
		{"issue_classified", "brief_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "sha256:" + "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"},
		{"issue_reviewed", "brief_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "sha256:" + "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"},
		{"issue_owner_decided", "brief_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "sha256:" + "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"},
		{"issue_priority_set", "brief_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "sha256:" + "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"},
		{"issue_taken", "brief_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "sha256:" + "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"},
		{"issue_design_put", "design_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "05851c9c83bf"},
		{"issue_reviewed", "design_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "05851c9c83bf"},
		{"issue_owner_decided", "design_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "05851c9c83bf"},
		{"issue_taken", "design_sha256", "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311", "05851c9c83bf"},
		{"issue_submitted", "product", "screening", "Screening"},
		{"issue_submitted", "kind", "complaint", "bug"},
		{"issue_submitted", "channel", "intake_local", "email"},
		{"issue_submitted", "served_person", "person:p2", "p2"},
		{"issue_added", "channel", "owner_relay", "relay"},
		{"issue_asked", "purpose", "ready_to_try", "try"},
		{"issue_asked", "env", "stg", "staging"},
		{"issue_answered", "verdict", "partly", "maybe"},
		{"issue_answered", "channel", "console", "ui"},
		{"issue_answered", "person", "person:owner", "agent:dots-p2"},
		{"issue_classified", "risk_class", "medium", "huge"},
		{"issue_classified", "category", "data_not_shown", "bug"},
		{"issue_classified", "seat_report", sha, "report.md"},
		{"issue_reviewed", "check_kind", "close_check", "classification"},
		{"issue_reviewed", "result", "does_not", "ok"},
		{"issue_reviewed", "seat_report", sha, "sha256:short"},
		{"issue_owner_decided", "decision", "D2", "D0"},
		{"issue_priority_set", "priority", "declined", "p0"},
		{"issue_taken", "risk_class", "large", "xl"},
		{"issue_taken", "mission_state", "awaiting_owner", "done"},
		{"issue_taken", "exception", "fault_small", "answer_only"},
		{"issue_state_changed", "change", "park", "pause"},
		{"issue_state_changed", "from", "awaiting_user", "waiting"},
		{"issue_state_changed", "to", "parked", "paused"},
		{"issue_state_changed", "conclusion", "already_possible", "done"},
		{"issue_merged", "into", "BI-3", "M-3"},
		{"issue_merged", "merged_from", "BI-4", "bi-4"},
		{"issue_merged", "risk_class", "small", "tiny"},
		{"intake_client_set", "change", "revoke", "delete"},
		{"intake_client_set", "intake_client_id", "agent:dots-p2", "intake:dots-p2"},
		{"intake_client_set", "person_id", "person:p2", "person:P2"},
		{"intake_acked", "intake_client_id", "agent:grok-owner", "agent:"},
		{"intake_acked", "cursor", "812.40", "40"},
	} {
		covered[c.event+"."+c.field] = true
		if why := badValue(c.event, c.field, c.good); why != "" {
			t.Errorf("%s.%s refused %q: %s", c.event, c.field, c.good, why)
		}
		if badValue(c.event, c.field, c.bad) == "" {
			t.Errorf("%s.%s took %q", c.event, c.field, c.bad)
		}
		if _, listed := fieldValues[c.event][c.field]; !listed {
			t.Errorf("%s.%s is not checked", c.event, c.field)
		}
	}
	// Every checked field has its pair above (review 334 B3) and is in its
	// event's body, so no check sits on a field no event has.
	for event, fields := range fieldValues {
		for name := range fields {
			if !covered[event+"."+name] {
				t.Errorf("%s.%s is checked but has no good and bad value here", event, name)
			}
			found := false
			for _, f := range bodies[event] {
				found = found || f.name == name
			}
			if !found {
				t.Errorf("%s.%s is checked but not in the body", event, name)
			}
		}
	}
	// A list is checked element by element.
	if badValue("attempt_checkpointed", "artifacts", []any{sha, "x"}) == "" || badValue("attempt_checkpointed", "artifacts", []string{sha, "x"}) == "" {
		t.Error("a list with a bad element was taken")
	}
	// Through checkBody: refused as the request's error, after the kind.
	err := checkBody("task_state_changed", map[string]any{"from": "ready", "to": "stuck", "cause": "x"})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), `field "to" is "stuck"`) {
		t.Fatalf("a bad state: %v", err)
	}
	if err = checkBody("task_state_changed", map[string]any{"from": "ready", "to": "leased"}); err != nil {
		t.Fatalf("a good state: %v", err)
	}
	// A wait's ref is the id of what it waits on (waits.go).
	wait := func(w, ref string) error {
		return checkBody("wait_limit_passed", map[string]any{"wait": w, "ref": ref, "since": "2026-10-21T12:00:00Z", "limit_s": 1800,
			"basis": "floor", "waited_s": 1801, "calls": "thread:s1"})
	}
	for w, ref := range map[string]string{"attempt_running": "a_" + ulid, "task_ready": "t_" + ulid, "message_unread": "m_" + ulid} {
		if err = wait(w, ref); err != nil {
			t.Errorf("%s on %s: %v", w, ref, err)
		}
	}
	for w, ref := range map[string]string{"attempt_running": "t_" + ulid, "task_ready": "m_" + ulid, "message_unread": "a_" + ulid} {
		if err = wait(w, ref); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s on %s was taken: %v", w, ref, err)
		}
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
		{"attempt_computed", "conclusion", []string{"done", "failed", "none"}},
		{"attempt_computed", "end_how", []string{"cancelled", "daemon_restart", "exit"}},
		{"attempt_ended", "end_how", []string{"cancelled", "daemon_restart", "exit", "lease_lost"}},
		{"attempt_leased", "executor", []string{"exec:claude", "exec:codex", "exec:cursor", "exec:deterministic"}},
		{"attempt_stale", "lease_expires_at", []string{"2026-10-15T19:20:32.540017Z", "2026-10-15T21:48:05.804471Z"}},
		{"budget_charged", "attempt_id", []string{"a_01M58P8W4S51ADQY493VJX8GV9", "a_01M58NBTAAP02QGTYZPQEG4A7Z"}},
		{"capability_scored", "source", []string{"benchmark", "in_job"}},
		{"capability_scored", "started_at", []string{"2026-10-19T02:14:41.057074107Z", "2026-10-19T06:05:04.538749668Z"}},
		{"capability_scored", "expires_at", []string{"2026-11-15T00:31:05.706388Z", "2026-11-14T19:24:35.169969Z"}},
		{"control_set", "kind", []string{"stop"}},
		{"control_set", "scope", []string{"mission", "task"}},
		{"host_registered", "host_kind", []string{"dev"}},
		{"message_acked", "message_id", []string{"m_01M57ZRW6FVCJ027DSNMQYW98G", "m_01M5A3Z1V3WVNEJ6CKBCFQXSHD"}},
		{"message_posted", "kind", []string{"answer", "ask", "note"}},
		{"mission_state_changed", "from", []string{"active", "proposed"}},
		{"mission_state_changed", "to", []string{"active", "done"}},
		{"pool_measured", "resets_at", []string{"2026-10-19T13:00:00.000000Z", "2026-10-24T20:40:42.000000Z"}},
		{"pool_measured", "source", []string{"reading"}},
		{"route_decided", "fingerprint", []string{"sha256:00148945974101bae5cc56d9eec73d8552110ece429aff92016d4b0739e25874"}},
		{"task_proposed", "role", []string{"brief", "implement", "plan", "review", "verify"}},
		{"task_state_changed", "from", []string{"leased", "proposed", "ready", "rework", "running", "submitted", "verifying"}},
		{"task_state_changed", "to", []string{"accepted", "blocked", "leased", "ready", "rework", "running", "submitted", "verifying"}},
		{"verification_recorded", "env", []string{"stg"}},
		{"verification_recorded", "method", []string{"deterministic", "review"}},
		{"verification_recorded", "verdict", []string{"fail", "pass"}},
		{"verification_recorded", "verification_id", []string{"v_01M511X7Z4QKQVQYT63H4QAVY8", "v_01M50GC0EPTT78523ZM35CAB1Y"}},
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
	for _, v := range []any{1, int64(1) << 62, json.Number("42")} {
		if !ofKind(v, kInt) {
			t.Errorf("%#v was refused", v)
		}
	}
	for _, v := range []any{1.5, 2.0, json.Number("1.5"), json.Number("1e3"), json.Number("9223372036854775808"), "7", nil, true} {
		if ofKind(v, kInt) {
			t.Errorf("%#v was taken as an integer", v)
		}
	}
	if err := checkBody("attempt_checkpointed", map[string]any{"checkpoint_seq": 2.0}); err == nil || !strings.Contains(err.Error(), "wrong kind") {
		t.Fatalf("a whole float: %v", err)
	}
}
