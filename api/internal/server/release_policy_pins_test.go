package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const (
	platformProd = "bifrost-deliver-platform-prod"
	platformStg  = "bifrost-deliver-platform"
)

// newPinnedHarness signs a policy that pins bifrost-deliver-platform-prod
// the way the signed paths.json does (revision and params.uiRevision), with
// STG having shipped plat and ui and PROD still on older commits.
func newPinnedHarness(t *testing.T) (h *policyHarness, plat, ui string) {
	t.Helper()
	h = newPolicyHarness(t)
	signedAt := time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC)
	var doc map[string]any
	if err := json.Unmarshal([]byte(rptest.PolicyText("rp-pins", signedAt, 90, platformProd)), &doc); err != nil {
		t.Fatal(err)
	}
	doc["ci_repos"] = []string{"bifrost-platform"}
	doc["pinned"] = map[string]any{platformProd: map[string]any{
		"from":   platformStg,
		"params": map[string]string{"revision": "bifrost-platform", "uiRevision": "bifrost-ui"},
	}}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw) + "\n"
	h.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{
		"policy.yaml": text,
		"policy.sig":  h.key.Sign(t, []byte(text), releasepolicy.NamespacePolicy),
	})
	plat, ui = rptest.SHA("plat"), rptest.SHA("ui")
	h.facts.Records = map[string]map[string]string{
		platformStg:  {"bifrost-platform": plat, "bifrost-ui": ui},
		platformProd: {"bifrost-platform": rptest.SHA("old-plat"), "bifrost-ui": rptest.SHA("old-ui")},
	}
	h.facts.Green = map[string]bool{"bifrost-platform@" + plat: true}
	h.facts.Files = map[string][]string{"bifrost-platform": {"docs/readme.md"}, "bifrost-ui": {"src/Button.tsx"}}
	return h, plat, ui
}

func runRequest(pipeline, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/pipelines/"+pipeline+"/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(approvals.SessionHeader, "sess-rp")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", pipeline)
	return req.WithContext(withRoute(req, rctx))
}

// A PROD release names the ui commit in its nested params. Each path a
// request takes hands the engine what the catalog's own Extract or
// Normalize produced, never a hand-built map, and the engine must see both
// pins. Before this test the engine read the nested params in a shape no
// real request arrives in, so params.uiRevision was never pinned.
func TestPinsInNestedParamsReachTheEngineOnEveryPath(t *testing.T) {
	h, plat, ui := newPinnedHarness(t)
	body := `{"revision":"` + plat + `","who":"agent@mac","params":{"uiRevision":"` + ui + `"}}`
	want := "bifrost-platform=" + plat + ",bifrost-ui=" + ui

	// The catalog's Extract on a real request body, straight into the engine.
	act, ok := actions.ByID("start_pipeline_run")
	if !ok {
		t.Fatal("start_pipeline_run is not in the catalog")
	}
	req := runRequest(platformProd, body)
	params, err := act.Extract(req, []byte(body))
	if err != nil || act.Missing(params) != "" {
		t.Fatalf("extract = %v %v missing=%q", params, err, act.Missing(params))
	}
	d := h.srv.releasePolicy.Decide(req.Context(), "start_pipeline_run", act.TierOf(req.Context(), params), params)
	if !d.Auto || d.SHAs["bifrost-ui"] != ui || d.SHAs["bifrost-platform"] != plat || !strings.Contains(d.ClauseText(), "pinned_to:"+platformStg) {
		t.Fatalf("extracted params: auto = %v sha = %s clauses = %s reasons = %v", d.Auto, d.SHAText(), d.ClauseText(), d.Reasons)
	}

	// The trial route.
	query := "revision=" + plat + "&who=agent%40mac&params=" + url.QueryEscape(`{"uiRevision":"`+ui+`"}`)
	code, out, raw := h.check(t, platformProd, query)
	if code != http.StatusOK || !out.Covered || out.SHAs["bifrost-ui"] != ui || out.SHAs["bifrost-platform"] != plat {
		t.Fatalf("trial = %d %s", code, raw)
	}

	// A request for an approval is sent back to the direct route.
	create := httptest.NewRequest(http.MethodPost, "/api/v1/approvals", strings.NewReader(
		`{"action":"start_pipeline_run","params":{"name":"`+platformProd+`","revision":"`+plat+`","who":"agent@mac","params":{"uiRevision":"`+ui+`"}},"reason":"ship"}`))
	create.Header.Set(approvals.SessionHeader, "sess-rp")
	rec := httptest.NewRecorder()
	h.srv.approvals.HandleCreate(rec, create)
	var answer map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &answer)
	if rec.Code != http.StatusBadRequest || answer["auto_approved_by"] != "rp-pins" {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}

	// The direct route starts the run and audits both commits.
	rec = httptest.NewRecorder()
	h.srv.guard("start_pipeline_run", func(w http.ResponseWriter, _ *http.Request) {
		h.hits++
		w.WriteHeader(http.StatusCreated)
	})(rec, runRequest(platformProd, body))
	if rec.Code != http.StatusCreated || h.hits != 1 {
		t.Fatalf("direct = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
	if a := h.auditText(t); !strings.Contains(a, "sha="+want) {
		t.Fatalf("audit lacks sha=%s:\n%s", want, a)
	}

	// Without the ui pin the release waits, on every path.
	bare := `{"revision":"` + plat + `","who":"agent@mac"}`
	rec = httptest.NewRecorder()
	h.srv.guard("start_pipeline_run", func(http.ResponseWriter, *http.Request) { h.hits++ })(rec, runRequest(platformProd, bare))
	if rec.Code != http.StatusForbidden || h.hits != 1 || !strings.Contains(rec.Body.String(), "bifrost-ui: the request pins no commit for it") {
		t.Fatalf("no ui pin = %d hits=%d %s", rec.Code, h.hits, rec.Body.String())
	}
}
