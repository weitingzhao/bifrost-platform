//bifrost:owner delivery

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func TestDeliveryBoundariesRoute(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	if code, _ := call(t, s, "", "GET", "delivery?view=boundaries", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", code)
	}
	if code, out := call(t, s, "viewer", "GET", "delivery?view=boundaries", nil); code != http.StatusOK || out["as_of"] == nil || out["tuning"] == nil {
		t.Fatalf("snapshot: %d %v", code, out)
	}
	if code, _ := call(t, s, "viewer", "GET", "delivery?view=boundaries&task=x", nil); code != http.StatusBadRequest {
		t.Fatalf("mixed query: %d", code)
	}
	if code, _ := call(t, s, "viewer", "GET", "delivery", nil); code != http.StatusBadRequest {
		t.Fatalf("legacy query: %d", code)
	}
	for _, selector := range []string{"task", "commit"} {
		if code, _ := call(t, s, "viewer", "GET", "delivery?view=configuration&"+selector+"=x", nil); code != http.StatusBadRequest {
			t.Fatalf("configuration mixed query %s: %d", selector, code)
		}
	}
}

// Moved from agentrt/store when the projection left it (2026-11-18, decoupling
// item 5); it was store.TestDeliveryBoundariesVersionsAndPreview, which read
// the view through the store and now reads it through the route. A version
// scheduled for later is not in force, an expired one does not fall back, a
// retired kind is named and never parsed, and the UI's JSON draft checks like
// the rule's text.
func TestDeliveryBoundariesVersionsAndPreview(t *testing.T) {
	pool, _ := world(t)
	key := rptest.NewKey(t)
	anchor = key.Fingerprint
	defer func() { anchor = "" }()
	s := server(t, pool)
	withFloor := server(t, pool, func(a *agentrtAPI) { a.DiskFloor = 12345 })
	now := time.Now().UTC()
	past := now.Add(-time.Hour).Format(time.RFC3339)
	future := now.Add(time.Hour).Format(time.RFC3339)
	expires := now.Add(24 * time.Hour).Format(time.RFC3339)
	text := func(version int, direction, effective, exp, body string) string {
		return "rule_id: tuning\nrule_version: " + string(rune('0'+version)) + "\ndirection: " + direction + "\neffective_at: " + effective + "\nexpires_at: " + exp + "\n" + body
	}
	sign := func(txt string) {
		t.Helper()
		if code, out := call(t, s, "admin", "POST", "rules/tuning/sign", map[string]any{"text": txt, "signature": key.Sign(t, []byte(txt), rules.Namespace)}); code != http.StatusOK {
			t.Fatalf("sign: %d %v", code, out)
		}
	}
	readFrom := func(s *httptest.Server) map[string]any {
		t.Helper()
		code, out := call(t, s, "viewer", "GET", "delivery?view=boundaries", nil)
		if code != http.StatusOK {
			t.Fatalf("boundaries: %d %v", code, out)
		}
		return out
	}
	read := func() map[string]any { return readFrom(s) }
	rule := func(doc map[string]any, id string) map[string]any {
		for _, r := range doc["rules"].([]any) {
			if m := r.(map[string]any); m["rule_id"] == id {
				return m
			}
		}
		return nil
	}
	_, err := pool.Exec(ctx, agentrttest.ConcurrencyRule)
	must(t, err)
	doc := read()
	if len(doc["tuning"].([]any)) != len(rules.TuningKeys()) || doc["disk_floor_bytes"] != float64(15<<30) {
		t.Fatalf("defaults: %v", doc)
	}
	for _, v := range doc["tuning"].([]any) {
		if m := v.(map[string]any); m["value"] != m["default"] || m["source"] != "code_default" {
			t.Fatalf("default: %v", m)
		}
	}
	if r := rule(doc, "concurrency"); r == nil || r["status"] != "retired" || r["in_force"] != nil || r["latest"] != nil {
		t.Fatalf("the retired concurrency rule is not listed as retired: %v", doc["rules"])
	}
	sign(text(1, rules.Initial, past, expires, "values: {failures_to_alert: 3}\n"))
	row := rule(read(), "tuning")
	ver := func(m map[string]any, k string) any {
		if d, ok := m[k].(map[string]any); ok {
			return d["rule_version"]
		}
		return nil
	}
	if ver(row, "in_force") != float64(1) || ver(row, "draft") != float64(2) || ver(row, "latest") != float64(1) {
		t.Fatalf("versions: %v", row)
	}
	// The UI's JSON draft must parse identically to the rule's YAML text.
	draft := row["draft"].(map[string]any)
	draft["values"] = map[string]any{"failures_to_alert": 5}
	b, err := json.Marshal(draft)
	must(t, err)
	if code, out := call(t, s, "viewer", "POST", "rules/tuning/check", map[string]any{"text": string(b)}); code != http.StatusOK || out["apply_via"] != "capacity_approval" {
		t.Fatalf("capacity: %d %v", code, out)
	}
	draft["expires_at"] = now.Add(48 * time.Hour).Format(time.RFC3339)
	b, err = json.Marshal(draft)
	must(t, err)
	if code, out := call(t, s, "viewer", "POST", "rules/tuning/check", map[string]any{"text": string(b)}); code != http.StatusOK || out["apply_via"] != "signature" {
		t.Fatalf("authority: %d %v", code, out)
	}
	sign(text(2, rules.Loosen, future, expires, "values: {failures_to_alert: 5}\n"))
	doc = readFrom(withFloor)
	row = rule(doc, "tuning")
	if ver(row, "in_force") != float64(1) || ver(row, "latest") != float64(2) || ver(row, "draft") != float64(3) || doc["disk_floor_bytes"] != float64(12345) {
		t.Fatalf("scheduled version is not effective: %v", row)
	}
	if code, out := call(t, s, "viewer", "POST", "rules/tuning/check", map[string]any{"text": string(b)}); code == http.StatusOK || !strings.Contains(strings.ToLower(toString(out)), "at version 2") {
		t.Fatalf("a stale draft checked: %d %v", code, out)
	}
	sign(text(3, rules.Loosen, past, now.Add(-time.Minute).Format(time.RFC3339), "values: {failures_to_alert: 5}\n"))
	if r := rule(read(), "tuning"); r["in_force"] != nil || r["status"] != "not_in_force" {
		t.Fatalf("expired rule fell back: %v", r)
	}
}

func toString(v any) string { b, _ := json.Marshal(v); return string(b) }

func frozenBoundaryRule(id string, current, latest *store.StoredRule, now time.Time) deliveryview.BoundaryRule {
	v := deliveryview.BoundaryRule{ID: id, Status: "not_in_force", Notes: []string{}}
	if current != nil {
		v.Current = current.Doc()
		v.Status = "in_force"
	}
	if latest != nil {
		v.Latest = latest.Doc()
		v.Draft = latest.Doc()
		v.Draft["rule_version"] = latest.Version + 1
		v.Draft["direction"] = rules.Loosen
		if latest.Effective.After(now) {
			v.Notes = append(v.Notes, "The latest version is scheduled; the effective version is shown separately.")
		}
		if !latest.Expires.After(now) {
			v.Notes = append(v.Notes, "The latest version has expired. Review its expiry before proposing a replacement.")
		}
	}
	if id == rules.KindTuning && latest == nil {
		v.Status = "code_defaults"
		values := map[string]int{}
		for _, key := range rules.TuningKeys() {
			values[key.Name] = key.Default
		}
		v.Draft = map[string]any{"rule_id": id, "rule_version": 1, "direction": rules.Initial, "effective_at": now.UTC().Format(time.RFC3339), "expires_at": "", "values": values}
		v.Notes = append(v.Notes, "No tuning rule is installed. Defaults below are read from the runtime registry. The first version requires a signature and an explicit expiry.")
	}
	return v
}

// Moved from agentrt/store with the projection (2026-11-18).
func TestBoundaryProjectionFrozenParity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{rules.KindTuning, rules.KindPlan, "unknown"} {
		for _, c := range []bool{false, true} {
			for _, l := range []bool{false, true} {
				for _, offset := range []time.Duration{-time.Hour, 0, time.Hour} {
					var current, latest *store.StoredRule
					makeRule := func() *store.StoredRule {
						return &store.StoredRule{Rule: rules.Rule{Header: rules.Header{ID: id, Version: 3, Direction: rules.Initial, Effective: now.Add(offset), Expires: now.Add(offset)}, Kind: rules.KindTuning, Tuning: &rules.Tuning{Values: map[string]int{"failures_to_alert": 3}}}}
					}
					if c {
						current = makeRule()
					}
					if l {
						latest = makeRule()
					}
					want := frozenBoundaryRule(id, current, latest, now)
					got := boundaryRuleFrom(id, current, latest, now)
					if !reflect.DeepEqual(want, got) {
						t.Fatalf("%s %v %v %v: %#v != %#v", id, c, l, offset, want, got)
					}
					oldJSON, _ := json.Marshal(want)
					newJSON, _ := json.Marshal(got)
					if !bytes.Equal(oldJSON, newJSON) {
						t.Fatal("json differs")
					}
					if got.Draft != nil && got.Latest != nil {
						got.Draft["rule_version"] = 99
						if got.Latest["rule_version"] == 99 {
							t.Fatal("draft aliases latest")
						}
					}
				}
			}
		}
	}
}

// Origin: 2026-11-21 decoupling line: delivery reaches owner through a port instead of importing approvals
// and questions. What the store's words mean moved here: a rejected status is
// Rejected, an answer on the rule's channel is ByRule, an unanswered question
// is no answer; a read that fails stays an error.
func TestReleaseContextReadsOwnerRecordsThroughThePort(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	seen, err := approvalsSeen(func() ([]approvals.Approval, error) {
		return []approvals.Approval{
			{ID: "r", Number: 7, Action: "start_pipeline_run", Status: approvals.StatusRejected, RejectReason: "no", DecidedAt: at, Params: map[string]any{"name": "x"}, KeyParams: map[string]string{"repo": "y"}},
			{ID: "e", Status: approvals.StatusExpired, DecidedAt: at},
			{ID: "a", Status: approvals.StatusExecuted, Channel: "phone"},
		}, nil
	})()
	if err != nil || len(seen) != 3 || !seen[0].Rejected || seen[0].RejectReason != "no" || seen[0].Number != 7 || seen[0].Params["name"] != "x" || seen[0].KeyParams["repo"] != "y" ||
		seen[1].Rejected || seen[2].Rejected || seen[2].Channel != "phone" {
		t.Fatalf("approvals: %+v, %v", seen, err)
	}
	if _, err = approvalsSeen(func() ([]approvals.Approval, error) { return nil, errors.New("disk") })(); err == nil {
		t.Fatal("an unreadable approval store read as empty")
	}
	var open, owner, rule questions.Question
	_ = json.Unmarshal([]byte(`{"id":"q0"}`), &open)
	_ = json.Unmarshal([]byte(`{"id":"q1","answer":{"by":"owner","at":"2026-11-21T12:00:00Z","channel":"phone"}}`), &owner)
	_ = json.Unmarshal([]byte(`{"id":"q2","answer":{"by":"rule","at":"2026-11-21T12:00:00Z","channel":"`+questions.RuleChannel+`"}}`), &rule)
	answers, err := answersSeen(func() ([]questions.Question, error) { return []questions.Question{open, owner, rule}, nil })()
	if err != nil || len(answers) != 2 || answers[0].ByRule || !answers[0].At.Equal(at) || !answers[1].ByRule {
		t.Fatalf("answers: %+v, %v", answers, err)
	}
	if _, err = answersSeen(func() ([]questions.Question, error) { return nil, errors.New("disk") })(); err == nil {
		t.Fatal("an unreadable question store read as empty")
	}
}
