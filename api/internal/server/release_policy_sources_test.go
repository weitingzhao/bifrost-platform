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

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

// applyHarness is the policy harness with a plan source: one plan of one
// Pipeline in cicd that the applier applied before. withSection signs a
// policy that has an apply section; without it the policy is the one the
// harness always signed. live is the cluster's copy of that Pipeline.
func applyHarness(t *testing.T, withSection bool) (*policyHarness, *rptest.Live) {
	t.Helper()
	h := newPolicyHarness(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if withSection {
		text := rptest.PolicyTextWith("rp-apply", now.Add(-time.Hour), 90, func(doc map[string]any) {
			doc["apply"] = map[string]any{
				"namespaces": []string{"cicd"}, "kinds": []string{"Pipeline", "Task"},
				"exclude_paths": []string{"k8s/cicd/release-policy/**"}, "field_manager": "the-applier", "require_previous_generation": true,
			}
		}, policyPipeline)
		h.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{"policy.yaml": text, "policy.sig": h.key.Sign(t, []byte(text), releasepolicy.NamespacePolicy)})
	}
	plan := &workactions.Summary{
		Ready: true, Policy: "pass", Repo: "repo-ops", Path: "k8s/cicd/tekton/pipeline-ci.yaml", Commit: rptest.SHA("ops"),
		Objects: []workactions.Object{{Namespace: "cicd", Kind: "Pipeline", Name: "ci", Ownership: "ssa"}},
	}
	live := &rptest.Live{Objects: map[string][]rptest.LiveManager{"cicd/Pipeline/ci": {{Manager: "the-applier", Operation: "Apply"}}}}
	h.srv.releasePolicy = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: h.cms, Writer: rptest.Writer{CMs: h.cms}, Git: h.facts, CI: h.facts, Window: h.facts, Deployed: h.facts,
		Plans: policyPlans{summarize: func(_ context.Context, id string) (workactions.Summary, error) {
			if id != "plan-1" {
				return workactions.Summary{}, errors.New("not found")
			}
			return *plan, nil
		}},
		Live: policyLive{managers: func(_ context.Context, namespace, kind, name string) ([]workactions.ManagedBy, bool, error) {
			list, found, err := live.Read(namespace, kind, name)
			out := make([]workactions.ManagedBy, 0, len(list))
			for _, m := range list {
				out = append(out, workactions.ManagedBy{Manager: m.Manager, Operation: m.Operation})
			}
			return out, found, err
		}},
		Anchor: h.key.Fingerprint, Now: func() time.Time { return now },
	})
	h.srv.approvals.SetAutoApprover(h.srv.releasePolicyApprover())
	return h, live
}

func (h *policyHarness) directApply(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/actuation/manifests/apply", strings.NewReader(`{"plan_id":"plan-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(approvals.SessionHeader, "sess-rp")
	rec := httptest.NewRecorder()
	h.srv.guard("apply_manifest", func(w http.ResponseWriter, r *http.Request) {
		h.hits++
		w.WriteHeader(http.StatusAccepted)
	})(rec, req)
	return rec
}

func (h *policyHarness) createApply(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals", strings.NewReader(`{"action":"apply_manifest","params":{"plan_id":"plan-1"},"reason":"apply"}`))
	req.Header.Set(approvals.SessionHeader, "sess-rp")
	rec := httptest.NewRecorder()
	h.srv.approvals.HandleCreate(rec, req)
	return rec
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 4. A tier C apply the signed
// policy covers runs on the direct call and is audited as a release is; the
// approval route answers "call directly". The same apply of an object the
// cluster does not have keeps the card.
func TestPolicyCoversADirectApplyAndAudits(t *testing.T) {
	if tier := mustAction(t, "apply_manifest").TierOf(context.Background(), map[string]any{"plan_id": "plan-1"}); tier != actions.TierC {
		t.Skipf("apply_manifest classifies as %s here, not C", tier)
	}
	h, live := applyHarness(t, true)
	rec := h.directApply(t)
	if rec.Code != http.StatusAccepted || h.hits != 1 {
		t.Fatalf("direct = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
	want := "auto-approved policy_id=rp-apply requester=sess-rp sha=repo-ops=" + rptest.SHA("ops") +
		" clauses=signature,unexpired,not_frozen,plan_passed,apply_namespaces,apply_kinds,apply_objects,apply_paths,previous_generation path=repo-ops:k8s/cicd/tekton/pipeline-ci.yaml"
	if a := h.auditText(t); !strings.Contains(a, want) || !strings.Contains(a, "release_policy.auto_approve") || !strings.Contains(a, "apply_manifest plan-1") {
		t.Fatalf("audit lacks %q:\n%s", want, a)
	}
	c := h.createApply(t)
	var body map[string]any
	_ = json.Unmarshal(c.Body.Bytes(), &body)
	if c.Code != http.StatusBadRequest || body["error"] != "call directly" || body["auto_approved_by"] != "rp-apply" {
		t.Fatalf("create = %d %s", c.Code, c.Body.String())
	}

	// One thing wrong: the cluster has no such object, so this is a first creation.
	delete(live.Objects, "cicd/Pipeline/ci")
	rec = h.directApply(t)
	if rec.Code != http.StatusForbidden || h.hits != 1 || !strings.Contains(rec.Body.String(), "no previous generation to go back to") {
		t.Fatalf("a new object: direct = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
	if c := h.createApply(t); c.Code != http.StatusCreated {
		t.Fatalf("a new object: create = %d %s", c.Code, c.Body.String())
	}
	if n := strings.Count(h.auditText(t), "release_policy.auto_approve"); n != 1 {
		t.Fatalf("auto-approvals audited = %d, want the first alone", n)
	}
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 4. Under the policy signed
// before the apply section existed, the direct call's refusal is the body it
// always was, to the byte: no release_policy reasons, nothing audited.
func TestAPolicyWithoutAnApplySectionAnswersAnApplyAsBefore(t *testing.T) {
	if tier := mustAction(t, "apply_manifest").TierOf(context.Background(), map[string]any{"plan_id": "plan-1"}); tier != actions.TierC {
		t.Skipf("apply_manifest classifies as %s here, not C", tier)
	}
	h, _ := applyHarness(t, false)
	rec := h.directApply(t)
	if rec.Code != http.StatusForbidden || h.hits != 0 {
		t.Fatalf("direct = %d hits=%d", rec.Code, h.hits)
	}
	if got, want := strings.TrimSpace(rec.Body.String()), `{"action":"apply_manifest","error":"approval required"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	if c := h.createApply(t); c.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", c.Code, c.Body.String())
	}
	if a := h.auditText(t); strings.Contains(a, "release_policy") {
		t.Fatalf("the policy was audited for an apply it does not judge:\n%s", a)
	}
}

func mustAction(t *testing.T, id string) actions.Action {
	t.Helper()
	act, ok := actions.ByID(id)
	if !ok {
		t.Fatalf("%s is not in the catalog", id)
	}
	return act
}

func reviewRequest(session, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/release-policy/reviews", strings.NewReader(body))
	if session != "" {
		req.Header.Set(approvals.SessionHeader, session)
	}
	return req
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 3. The review route takes a
// repository, a full commit, files and a verdict, records the caller's
// session as the reviewer whatever the body says, and what it recorded is
// what the release policy's source reads back.
func TestRecordReleaseReview(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	good := `{"repo":"bifrost-platform","commit":"` + commit + `","files":["api/a.go","api/b.go"],"verdict":"passed","not_read":"the tests","reviewer":"local_forged"}`
	for name, tc := range map[string]struct{ session, body string }{
		"no body":          {"local_r", ``},
		"a branch":         {"local_r", `{"repo":"bifrost-platform","commit":"main","files":["a"],"verdict":"passed"}`},
		"not a repo":       {"local_r", `{"repo":"../x","commit":"` + commit + `","files":["a"],"verdict":"passed"}`},
		"unknown verdict":  {"local_r", `{"repo":"r","commit":"` + commit + `","files":["a"],"verdict":"ok"}`},
		"no files":         {"local_r", `{"repo":"r","commit":"` + commit + `","files":[],"verdict":"passed"}`},
		"an absolute path": {"local_r", `{"repo":"r","commit":"` + commit + `","files":["/etc/x"],"verdict":"passed"}`},
		"no session":       {"", good},
		"not a session id": {"two words", good},
	} {
		w := httptest.NewRecorder()
		(&Server{}).handleRecordReleaseReview(w, reviewRequest(tc.session, tc.body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	(&Server{}).handleRecordReleaseReview(w, reviewRequest("local_r", good))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a platform with no ledger answered %d", w.Code)
	}

	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	srv := &Server{agentRuntime: &agentRuntime{api: &agentrtapi.API{DB: pool}}}
	w = httptest.NewRecorder()
	srv.handleRecordReleaseReview(w, reviewRequest("local_r", good))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != http.StatusOK || out["reviewer"] != "local_r" || out["verdict"] != "passed" {
		t.Fatalf("record = %d %s", w.Code, w.Body.String())
	}
	// The same thread failing it afterwards is a second, newer record.
	w = httptest.NewRecorder()
	srv.handleRecordReleaseReview(w, reviewRequest("local_r", strings.Replace(good, `"passed"`, `"failed"`, 1)))
	if w.Code != http.StatusOK {
		t.Fatalf("second record = %d %s", w.Code, w.Body.String())
	}
	got, err := policyReviews{db: func() store.DB { return pool }}.Reviews(context.Background(), "bifrost-platform", commit)
	if err != nil || len(got) != 2 {
		t.Fatalf("reviews = %+v err %v", got, err)
	}
	if got[0].Reviewer != "local_r" || !got[0].Passed || strings.Join(got[0].Files, ",") != "api/a.go,api/b.go" || got[0].NotRead != "the tests" || got[1].Passed {
		t.Fatalf("reviews = %+v", got)
	}
	// Another commit, and another repository, have none.
	for _, at := range [][2]string{{"bifrost-platform", strings.Repeat("b", 40)}, {"bifrost-ui", commit}} {
		if rows, err := evidence.Reviews(context.Background(), pool, at[0], at[1]); err != nil || len(rows) != 0 {
			t.Fatalf("%s@%s: %+v err %v", at[0], at[1], rows, err)
		}
	}
	// A review is not a CI verdict: the ledger's answer for the commit is none.
	if status, run, err := ledgerCI(func() store.DB { return pool }, nil)(context.Background(), "bifrost-platform", commit); err != nil || status != "" || run != "" {
		t.Fatalf("ledgerCI after a passed review = %q %q %v", status, run, err)
	}
	if _, err := (policyReviews{db: func() store.DB { return nil }}).Reviews(context.Background(), "bifrost-platform", commit); err == nil {
		t.Fatal("a ledger that is not connected read as no reviews")
	}
}
