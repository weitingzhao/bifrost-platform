package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// bifrost-deliver-research is a tier C pipeline (actions.ProdPipeline);
// bifrost-deliver-stg is tier B.
const (
	policyPipeline = "bifrost-deliver-research"
	stgPipeline    = "bifrost-deliver-stg"
)

type policyHarness struct {
	srv   *Server
	cms   *rptest.ConfigMaps
	facts *rptest.Facts
	key   rptest.Key
	hits  int
}

func newPolicyHarness(t *testing.T) *policyHarness {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	h := &policyHarness{key: rptest.NewKey(t), cms: rptest.NewConfigMaps()}
	text := rptest.PolicyText("rp-20261008-1100", now.Add(-time.Hour), 90, policyPipeline, stgPipeline)
	h.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{
		"policy.yaml": text,
		"policy.sig":  h.key.Sign(t, []byte(text), releasepolicy.NamespacePolicy),
	})
	h.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	newSHA := rptest.SHA("new")
	h.facts = &rptest.Facts{
		Heads:   map[string]string{"repo-app": newSHA},
		Files:   map[string][]string{"repo-app": {"docs/readme.md"}},
		Green:   map[string]bool{"repo-app@" + newSHA: true},
		Records: map[string]map[string]string{policyPipeline: {"repo-app": rptest.SHA("old")}, stgPipeline: {"repo-app": rptest.SHA("old")}},
	}
	audit := actuation.NewAuditLog(filepath.Join(dir, "audit.json"))
	h.srv = &Server{audit: audit, approvals: approvals.New(filepath.Join(dir, "approvals"), audit)}
	h.srv.releasePolicy = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: h.cms, Writer: rptest.Writer{CMs: h.cms}, Git: h.facts, CI: h.facts, Window: h.facts, Deployed: h.facts,
		Anchor: h.key.Fingerprint, Now: func() time.Time { return now },
	})
	h.srv.approvals.SetAutoApprover(h.srv.releasePolicyApprover())
	return h
}

func (h *policyHarness) direct(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	next := func(w http.ResponseWriter, r *http.Request) {
		h.hits++
		w.WriteHeader(http.StatusCreated)
	}
	body := `{"revision":"main","who":"agent@mac"}`
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(approvals.SessionHeader, "sess-rp")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/delivery/pipelines/"), "/runs"))
	req = req.WithContext(withRoute(req, rctx))
	rec := httptest.NewRecorder()
	h.srv.guard("start_pipeline_run", next)(rec, req)
	return rec
}

func withRoute(r *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
}

func (h *policyHarness) create(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"action":"start_pipeline_run","params":{"name":"` + policyPipeline + `","revision":"main","who":"agent@mac"},"reason":"ship"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals", strings.NewReader(body))
	req.Header.Set(approvals.SessionHeader, "sess-rp")
	rec := httptest.NewRecorder()
	h.srv.approvals.HandleCreate(rec, req)
	return rec
}

func (h *policyHarness) auditText(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.srv.audit.HandleList(rec, httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil))
	return rec.Body.String()
}

const runsPath = "/api/v1/delivery/pipelines/" + policyPipeline + "/runs"

func TestPolicyAutoApprovesDirectReleaseAndAudits(t *testing.T) {
	h := newPolicyHarness(t)
	rec := h.direct(t, runsPath)
	if rec.Code != http.StatusCreated || h.hits != 1 {
		t.Fatalf("direct = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
	want := "auto-approved policy_id=rp-20261008-1100 requester=sess-rp sha=repo-app=" + rptest.SHA("new") +
		" clauses=signature,unexpired,not_frozen,allow:" + policyPipeline
	if a := h.auditText(t); !strings.Contains(a, want) || !strings.Contains(a, "release_policy.auto_approve") {
		t.Fatalf("audit lacks %q:\n%s", want, a)
	}
	// The MCP path asks for an approval first; the policy sends it back to the
	// direct route without storing a pending record or paging the Owner.
	c := h.create(t)
	var body map[string]any
	_ = json.Unmarshal(c.Body.Bytes(), &body)
	if c.Code != http.StatusBadRequest || body["error"] != "call directly" || body["auto_approved_by"] != "rp-20261008-1100" {
		t.Fatalf("create = %d %s", c.Code, c.Body.String())
	}
	if n := len(h.srv.approvals.Pending()); n != 0 {
		t.Fatalf("pending approvals = %d", n)
	}
}

func TestPolicyRefusalsKeepTheApprovalPath(t *testing.T) {
	cases := map[string]func(t *testing.T, h *policyHarness){
		"expired": func(t *testing.T, h *policyHarness) {
			text := rptest.PolicyText("rp-old", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), 7, policyPipeline)
			h.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{"policy.yaml": text, "policy.sig": h.key.Sign(t, []byte(text), releasepolicy.NamespacePolicy)})
		},
		"bad signature": func(t *testing.T, h *policyHarness) {
			text := rptest.PolicyText("rp-x", time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC), 90, policyPipeline)
			h.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{"policy.yaml": text, "policy.sig": rptest.NewKey(t).Sign(t, []byte(text), releasepolicy.NamespacePolicy)})
		},
		"frozen": func(_ *testing.T, h *policyHarness) {
			h.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:00:00Z"})
		},
		"path hit": func(_ *testing.T, h *policyHarness) {
			h.facts.Files["repo-app"] = []string{"migrations/0002_add.sql"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h := newPolicyHarness(t)
			mutate(t, h)
			rec := h.direct(t, runsPath)
			if rec.Code != http.StatusForbidden || h.hits != 0 ||
				!strings.Contains(rec.Body.String(), "approval required") || !strings.Contains(rec.Body.String(), "release_policy") {
				t.Fatalf("direct = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
			}
			if c := h.create(t); c.Code != http.StatusCreated {
				t.Fatalf("create = %d %s", c.Code, c.Body.String())
			}
			if strings.Contains(h.auditText(t), "auto-approved") {
				t.Fatal("audit has an auto-approval")
			}
		})
	}
}

func TestPolicyNeverCoversTierD(t *testing.T) {
	h := newPolicyHarness(t)
	next := func(w http.ResponseWriter, r *http.Request) { h.hits++ }
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes/node-a/drain", strings.NewReader(`{"force":true}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "node-a")
	req = req.WithContext(withRoute(req, rctx))
	rec := httptest.NewRecorder()
	h.srv.guard("drain_node", next)(rec, req)
	if rec.Code != http.StatusForbidden || h.hits != 0 {
		t.Fatalf("drain = %d hits=%d", rec.Code, h.hits)
	}
}

const stgRunsPath = "/api/v1/delivery/pipelines/" + stgPipeline + "/runs"

func TestTierBReleaseRunsEitherWayAndIsAudited(t *testing.T) {
	h := newPolicyHarness(t)
	if rec := h.direct(t, stgRunsPath); rec.Code != http.StatusCreated || h.hits != 1 {
		t.Fatalf("covered stg = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
	a := h.auditText(t)
	if !strings.Contains(a, "release_policy.check") || !strings.Contains(a, `"covered"`) ||
		!strings.Contains(a, "clauses=signature,unexpired,not_frozen,allow:"+stgPipeline) {
		t.Fatalf("audit lacks the covered check:\n%s", a)
	}
	h.facts.Window = "no release window is open"
	if rec := h.direct(t, stgRunsPath); rec.Code != http.StatusCreated || h.hits != 2 {
		t.Fatalf("uncovered stg = %d hits=%d", rec.Code, h.hits)
	}
	if a := h.auditText(t); !strings.Contains(a, `"not covered"`) || !strings.Contains(a, "reasons=release window") {
		t.Fatalf("audit lacks the uncovered check:\n%s", a)
	}
	// No policy in force: tier B runs as before and nothing is audited.
	h.cms.Set(releasepolicy.PolicyConfigMap, nil)
	before := strings.Count(h.auditText(t), "release_policy.check")
	if rec := h.direct(t, stgRunsPath); rec.Code != http.StatusCreated || h.hits != 3 {
		t.Fatalf("stg without a policy = %d hits=%d", rec.Code, h.hits)
	}
	if after := strings.Count(h.auditText(t), "release_policy.check"); after != before {
		t.Fatalf("audited a check with no policy in force (%d -> %d)", before, after)
	}
}

func TestTierBReleaseStopsOnlyForASetFreeze(t *testing.T) {
	h := newPolicyHarness(t)
	h.cms.Set(releasepolicy.FreezeConfigMap, nil)
	if rec := h.direct(t, stgRunsPath); rec.Code != http.StatusCreated || h.hits != 1 {
		t.Fatalf("missing freeze ConfigMap = %d hits=%d", rec.Code, h.hits)
	}
	h.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:00:00Z", "who": "agent", "reason": "incident"})
	rec := h.direct(t, stgRunsPath)
	if rec.Code != http.StatusConflict || h.hits != 1 || !strings.Contains(rec.Body.String(), "releases are frozen") {
		t.Fatalf("frozen stg = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
	if a := h.auditText(t); !strings.Contains(a, "release_policy.frozen") {
		t.Fatalf("audit lacks the refusal:\n%s", a)
	}
}

func TestFreezeAndInstallRoutes(t *testing.T) {
	h := newPolicyHarness(t)
	call := func(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/release-policy/x", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	if rec := call(h.srv.handleReleaseFreeze, `{"who":"agent@mac","reason":"incident"}`); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"frozen":true`) {
		t.Fatalf("freeze = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h.srv.handleReleaseUnfreeze, `{"text":"unfreeze frozen_at=2026-10-08T12:00:00Z x\n","sig":"nope"}`); rec.Code != http.StatusConflict {
		t.Fatalf("unsigned unfreeze = %d %s", rec.Code, rec.Body.String())
	}
	text := "unfreeze frozen_at=2026-10-08T12:00:00Z at=2026-10-08T12:01:00Z by=owner\n"
	body, _ := json.Marshal(map[string]string{"text": text, "sig": h.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze)})
	if rec := call(h.srv.handleReleaseUnfreeze, string(body)); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"frozen":false`) {
		t.Fatalf("signed unfreeze = %d %s", rec.Code, rec.Body.String())
	}
	policy := rptest.PolicyText("rp-20261008-1159", time.Date(2026, 10, 8, 11, 59, 0, 0, time.UTC), 90, stgPipeline)
	body, _ = json.Marshal(map[string]string{"policy_yaml": policy, "policy_sig": rptest.NewKey(t).Sign(t, []byte(policy), releasepolicy.NamespacePolicy)})
	if rec := call(h.srv.handleReleasePolicyInstall, string(body)); rec.Code != http.StatusConflict {
		t.Fatalf("foreign install = %d %s", rec.Code, rec.Body.String())
	}
	body, _ = json.Marshal(map[string]string{"policy_yaml": policy, "policy_sig": h.key.Sign(t, []byte(policy), releasepolicy.NamespacePolicy)})
	if rec := call(h.srv.handleReleasePolicyInstall, string(body)); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"policy_id":"rp-20261008-1159"`) {
		t.Fatalf("install = %d %s", rec.Code, rec.Body.String())
	}
	a := h.auditText(t)
	for _, want := range []string{"release_policy.freeze", "release_policy.unfreeze", "release_policy.install"} {
		if !strings.Contains(a, want) {
			t.Fatalf("audit lacks %s:\n%s", want, a)
		}
	}
}
