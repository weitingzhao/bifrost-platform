package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func traceRequest(name, body string) *http.Request {
	r := httptest.NewRequest("POST", "/api/v1/delivery/pipelines/"+name+"/runs", strings.NewReader(body))
	route := chi.NewRouteContext()
	route.URLParams.Add("name", name)
	return r.WithContext(withRoute(r, route))
}

// Same catalog pipeline inputs as TestPipelineAndAppTiers, crossed with
// the policy/freeze/window cases of release_policy_test. Each pair executes
// the old guard path and the observed path, comparing the entire response
// and whether the executor was called, not just the policy's boolean.
func TestTracePreservesDecisions(t *testing.T) {
	t.Setenv("ACTION_TRACE_REQUIRED", "")
	names := []string{"bifrost-deliver-prod", "bifrost-deliver-platform-prod", policyPipeline,
		"bifrost-deliver-platform", stgPipeline, "bifrost-build-stg", "bifrost-build-research-dagster",
		"bifrost-build-research-pine", "bifrost-build-frontend-stg", "bifrost-build-flex-query",
		"bifrost-build-ib-gateway", "bifrost-build-market-data", "bifrost-ci-frontend", "bifrost-ci-platform",
		"bifrost-ci-python", "bifrost-clone-frontend-smoke", "bifrost-smoke", "not-a-pipeline-prod"}
	states := []string{"covered", "expired", "bad_signature", "frozen", "path_hit", "no_window", "foreign_window", "no_policy"}
	for _, name := range names {
		for _, state := range states {
			t.Run(name+"/"+state, func(t *testing.T) {
				h := newPolicyHarness(t)
				switch state {
				case "expired":
					text := rptest.PolicyText("rp-old", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), 7, policyPipeline)
					h.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{"policy.yaml": text, "policy.sig": h.key.Sign(t, []byte(text), releasepolicy.NamespacePolicy)})
				case "bad_signature":
					h.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{"policy.yaml": "invalid", "policy.sig": "invalid"})
				case "frozen":
					h.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:00:00Z"})
				case "path_hit":
					h.facts.Files["repo-app"] = []string{"migrations/0002_add.sql"}
				case "no_window":
					h.facts.Window = "no release window is open"
				case "foreign_window":
					h.facts.Window = "held by another session"
				case "no_policy":
					h.cms.Set(releasepolicy.PolicyConfigMap, nil)
				}
				body := `{"revision":"main","who":"agent@mac"}`
				hits := 0
				next := func(w http.ResponseWriter, r *http.Request) {
					hits++
					w.Header().Set("X-Test", "unchanged")
					writeJSON(w, 201, map[string]any{"ok": true})
				}
				before := httptest.NewRecorder()
				h.srv.guardUntraced("start_pipeline_run", next)(before, traceRequest(name, body))
				oldHits := hits
				hits = 0
				after := httptest.NewRecorder()
				h.srv.guard("start_pipeline_run", next)(after, traceRequest(name, body))
				if before.Code != after.Code || before.Body.String() != after.Body.String() || hits != oldHits || before.Header().Get("X-Test") != after.Header().Get("X-Test") {
					t.Fatalf("before=%d %s hits=%d; after=%d %s hits=%d", before.Code, before.Body, oldHits, after.Code, after.Body, hits)
				}
			})
		}
	}
}

func TestTraceRequiredCompatibilityAndSources(t *testing.T) {
	for _, strict := range []string{"", "true"} {
		for _, source := range []string{"agent", "script", "browser", "executor"} {
			for _, complete := range []bool{false, true} {
				t.Run(strict+"/"+source+"/"+map[bool]string{false: "missing", true: "complete"}[complete], func(t *testing.T) {
					t.Setenv("ACTION_TRACE_REQUIRED", strict)
					h := newPolicyHarness(t)
					body := `{}`
					if complete {
						body = `{"reason":" ship ","unchecked":"none","rollback":"previous SHA"}`
					}
					r := traceRequest(stgPipeline, body)
					switch source {
					case "agent":
						r.Header.Set("X-Bifrost-Session", "caller")
					case "browser":
						r.Header.Set("Sec-Fetch-Mode", "cors")
					case "executor":
						r = r.WithContext(withExecutor(r.Context()))
					}
					hits := 0
					rec := httptest.NewRecorder()
					h.srv.tracedAction("start_pipeline_run", func(w http.ResponseWriter, r *http.Request) { hits++; writeJSON(w, 202, map[string]any{"ok": true}) })(rec, r)
					refused := strict != "" && !complete && (source == "agent" || source == "script")
					if refused {
						if rec.Code != 400 || hits != 0 || !strings.Contains(rec.Body.String(), traceRequiredMessage) {
							t.Fatal(rec.Code, rec.Body, hits)
						}
					} else if rec.Code != 202 || hits != 1 {
						t.Fatal(rec.Code, rec.Body, hits)
					}
					if !refused && source != "executor" && !strings.Contains(h.auditText(t), "action_trace.append_failed") {
						t.Fatal("missing append failure alert")
					}
					if !refused && !complete && (source == "agent" || source == "script") && !strings.Contains(h.auditText(t), "action_trace.missing_fields") {
						t.Fatal("missing compatibility audit")
					}
				})
			}
		}
	}
}

func TestTraceRedactsWithoutDamagingJSON(t *testing.T) {
	value := map[string]any{"reason": "line one\nBearer abcdefghijklmnop\nline three", "params": map[string]any{"token": "private-value"}, "rollback": "revert commit"}
	raw, err := json.Marshal(redactTrace(value))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "abcdefghijklmnop") || strings.Contains(string(raw), "private-value") || !json.Valid(raw) {
		t.Fatal(string(raw))
	}
	if value["rollback"] != "revert commit" {
		t.Fatal(value)
	}
}

func TestTraceRuntimeRecords(t *testing.T) {
	db := agentrttest.Migrated(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = store.Seed(ctx, pool); err != nil {
		t.Fatal(err)
	}
	h := newPolicyHarness(t)
	h.srv.agentRuntime = &agentRuntime{late: &lateDB{}}
	h.srv.agentRuntime.late.open(pool, time.Now())
	t.Setenv("ACTION_TRACE_REQUIRED", "")
	for _, name := range []string{stgPipeline, policyPipeline} {
		rec := httptest.NewRecorder()
		h.srv.guard("start_pipeline_run", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 202, map[string]any{"run": map[string]any{"name": "run-" + name, "uid": "uid-" + name}})
		})(rec, traceRequest(name, `{"revision":"main","who":"agent@mac","reason":"ship","unchecked":"none","rollback":"previous"}`))
		if rec.Code != 202 {
			t.Fatal(rec.Code, rec.Body)
		}
	}
	for _, action := range []string{"start_pipeline_run", "apply_manifest"} {
		a := approvals.Approval{ID: "appr_test", Action: action, Reason: "approved verbatim", Rollback: "old commit", Requester: "claimed", RequesterThread: "thread", Params: map[string]any{"plan_id": "plan-one"}}
		c := h.srv.beginApprovalTrace(ctx, a)
		h.srv.endApprovalTrace(c, map[string]any{"run": "human-run"}, nil)
	}
	h.srv.recordHeadDecision(ctx, headrelease.Deliverable{}, headrelease.Outcome{Deliverable: "platform", Action: "waiting", Why: "CI pending"})
	for _, code := range []int{502, 504} {
		rec := httptest.NewRecorder()
		h.srv.tracedAction("start_pipeline_run", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, code, map[string]any{"error": "failed or uncertain"})
		})(rec, traceRequest(stgPipeline, `{}`))
		if rec.Code != code {
			t.Fatal(rec.Code)
		}
	}
	rows, err := pool.Query(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_stream_id='action-trace'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"object", "initiator", "reason", "decision", "unchecked", "rollback", "result", "request_hash", "version"} {
			if v[key] == nil {
				t.Fatalf("missing %s in %s", key, raw)
			}
		}
		count++
	}
	if rows.Err() != nil || count != 7 {
		t.Fatal(count, rows.Err())
	}
	c := h.srv.beginApprovalTrace(ctx, approvals.Approval{Action: "apply_manifest", Params: map[string]any{}})
	h.srv.endApprovalTrace(c, nil, errors.New("uncertain create"))
}
