package patrol

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func writeFixtureSkills(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"fleet.yaml": `
id: fleet-drift-scan
name: Fleet Drift Scan
description: drift
schedule: "0 3 * * *"
trust_level: L0
scope: fleet
timeout: 30
mcp_tools: [verify_mission_snapshot, get_connectivity_matrix]
prompt_template: scan fleet
`,
		"cert.yaml": `
id: cert-expiry-check
name: Cert Expiry Check
description: certs
schedule: "0 6 * * 1"
trust_level: L0
scope: cluster-tls
timeout: 30
mcp_tools: [get_cluster_summary, get_gitops_apps]
prompt_template: check certs
`,
		"stale.yaml": `
id: stale-pod-cleanup
name: Stale Pod Cleanup
description: pods
schedule: "0 4 * * *"
trust_level: L1
scope: cluster-workloads
timeout: 30
mcp_tools: [get_cluster_summary, delete_pod]
prompt_template: cleanup pods
`,
		"l2.yaml": `
id: reserved-l2
name: Reserved L2
description: reserved
schedule: "0 12 * * *"
trust_level: L2
scope: reserved
timeout: 30
mcp_tools: [get_ops_context]
prompt_template: do not run
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func testHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("PATROL_DISPATCH", "stub")
	skills := t.TempDir()
	writeFixtureSkills(t, skills)
	h, err := NewHandlerWithOptions(HandlerOptions{
		SkillsDir:  skills,
		StateDir:   t.TempDir(),
		Dispatcher: stubDispatcher{},
		Now:        func() time.Time { return time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC) },
		SyncScan:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func testRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/patrol/skills", h.HandleListSkills)
	r.Get("/patrol/skills/{id}", h.HandleGetSkill)
	r.Put("/patrol/skills/{id}/enable", h.HandleEnable)
	r.Get("/patrol/runs", h.HandleListRuns)
	r.Post("/patrol/trigger/{id}", h.HandleTrigger)
	r.Post("/patrol/webhook/{event}", h.HandleWebhook)
	return r
}

func TestHandlerListGetEnableTrigger(t *testing.T) {
	h := testHandler(t)
	rt := testRouter(h)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/patrol/skills", nil))
	if rec.Code != 200 {
		t.Fatalf("list status %d %s", rec.Code, rec.Body.String())
	}
	var listed SkillsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Skills) != 4 {
		t.Fatalf("skills=%d", len(listed.Skills))
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/patrol/skills/fleet-drift-scan", nil))
	if rec.Code != 200 {
		t.Fatalf("get status %d", rec.Code)
	}
	var one PatrolSkill
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.ID != "fleet-drift-scan" || one.TrustLevel != TrustL0 {
		t.Fatalf("%+v", one)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/patrol/skills/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown get status %d", rec.Code)
	}

	body := bytes.NewBufferString(`{"enabled":false}`)
	req := httptest.NewRequest(http.MethodPut, "/patrol/skills/fleet-drift-scan/enable", body)
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("enable status %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.Enabled {
		t.Fatal("expected disabled")
	}

	body = bytes.NewBufferString(`{"enabled":true}`)
	req = httptest.NewRequest(http.MethodPut, "/patrol/skills/fleet-drift-scan/enable", body)
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/patrol/trigger/fleet-drift-scan", nil))
	if rec.Code != 200 {
		t.Fatalf("trigger L0 %d %s", rec.Code, rec.Body.String())
	}
	var trig TriggerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &trig); err != nil {
		t.Fatal(err)
	}
	if trig.Status != StatusCompleted || trig.Result != ResultSuccess {
		t.Fatalf("L0 trigger %+v", trig)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/patrol/trigger/missing-skill", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown trigger status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/patrol/runs?limit=10", nil))
	var runs RunsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if runs.Total < 1 || len(runs.Runs) < 1 {
		t.Fatalf("runs %+v", runs)
	}
}

func TestHandlerTriggerL1CronEscalatesL2Blocked(t *testing.T) {
	h := testHandler(t)

	resp, code := h.execute(context.Background(), "stale-pod-cleanup", TriggerManual)
	if code != 200 || resp.Status != StatusCompleted || resp.Result != ResultSuccess {
		t.Fatalf("L1 manual %+v code=%d", resp, code)
	}

	resp, code = h.execute(context.Background(), "stale-pod-cleanup", TriggerCron)
	if code != 200 || resp.Status != StatusBlocked || resp.Result != ResultEscalated {
		t.Fatalf("L1 cron %+v code=%d", resp, code)
	}

	resp, code = h.execute(context.Background(), "reserved-l2", TriggerManual)
	if code != 200 || resp.Status != StatusBlocked || resp.Result != ResultEscalated {
		t.Fatalf("L2 %+v code=%d", resp, code)
	}
}

func TestSchedulerFiresWhenDue(t *testing.T) {
	t.Setenv("PATROL_DISPATCH", "stub")
	skills := t.TempDir()
	if err := os.WriteFile(filepath.Join(skills, "due.yaml"), []byte(`
id: due-soon
name: Due Soon
description: scheduler test
schedule: "5 1 * * *"
trust_level: L0
scope: test
timeout: 15
mcp_tools: [get_ops_context]
prompt_template: ping
`), 0o644); err != nil {
		t.Fatal(err)
	}
	var now atomic.Int64
	base := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC).Unix()
	now.Store(base)
	var fires atomic.Int32
	disp := dispatchFunc(func(_ context.Context, skill PatrolSkill, trigger Trigger, _ string) dispatchOutcome {
		if trigger != TriggerCron {
			t.Errorf("expected cron trigger, got %s", trigger)
		}
		fires.Add(1)
		return dispatchOutcome{Result: ResultSuccess, Status: StatusCompleted, Evidence: "due " + skill.ID}
	})
	h, err := NewHandlerWithOptions(HandlerOptions{
		SkillsDir: skills,
		StateDir:  t.TempDir(),
		Dispatcher: disp,
		Now: func() time.Time {
			return time.Unix(now.Load(), 0).UTC()
		},
		SyncScan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.scanDue()
	if fires.Load() != 0 {
		t.Fatalf("premature fire at 01:00, next should be 01:05")
	}
	now.Store(time.Date(2026, 8, 9, 1, 5, 0, 0, time.UTC).Unix())
	h.scanDue()
	if fires.Load() != 1 {
		t.Fatalf("fires=%d want 1", fires.Load())
	}
	h.scanDue()
	if fires.Load() != 1 {
		t.Fatalf("second scan should not re-fire same minute, fires=%d", fires.Load())
	}
}

type dispatchFunc func(ctx context.Context, skill PatrolSkill, trigger Trigger, prompt string) dispatchOutcome

func (f dispatchFunc) Dispatch(ctx context.Context, skill PatrolSkill, trigger Trigger, prompt string, _ progressFn) dispatchOutcome {
	return f(ctx, skill, trigger, prompt)
}

func TestHandlerAsyncTriggerShowsRunningThenCompletes(t *testing.T) {
	t.Setenv("PATROL_DISPATCH", "stub")
	block := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(block) }) }
	t.Cleanup(release)

	skills := t.TempDir()
	writeFixtureSkills(t, skills)
	h, err := NewHandlerWithOptions(HandlerOptions{
		SkillsDir: skills,
		StateDir:  t.TempDir(),
		Dispatcher: dispatchFunc(func(_ context.Context, _ PatrolSkill, _ Trigger, _ string) dispatchOutcome {
			<-block
			return dispatchOutcome{Result: ResultSuccess, Status: StatusCompleted, Evidence: "NOMINAL"}
		}),
		Now:      func() time.Time { return time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC) },
		SyncScan: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := testRouter(h)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/patrol/trigger/fleet-drift-scan", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("trigger status %d %s", rec.Code, rec.Body.String())
	}
	var trig TriggerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &trig); err != nil {
		t.Fatal(err)
	}
	if trig.Status != StatusStarted || trig.Result != ResultRunning || trig.RunID == "" {
		t.Fatalf("trigger %+v", trig)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/patrol/skills/fleet-drift-scan", nil))
	var skill PatrolSkill
	if err := json.Unmarshal(rec.Body.Bytes(), &skill); err != nil {
		t.Fatal(err)
	}
	if skill.LastResult != ResultRunning {
		t.Fatalf("last_result=%s want running", skill.LastResult)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/patrol/runs?limit=5", nil))
	var runs RunsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs.Runs) < 1 || runs.Runs[0].Result != ResultRunning {
		t.Fatalf("runs %+v", runs)
	}

	release()
	deadline := time.Now().Add(2 * time.Second)
	for {
		last := h.store.LastRun("fleet-drift-scan")
		if last != nil && last.Result == ResultSuccess && last.Evidence == "NOMINAL" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for completion last=%+v", last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHandlerWebhookDeliveryComplete(t *testing.T) {
	t.Setenv("PATROL_DISPATCH", "stub")
	skills := t.TempDir()
	writeFixtureSkills(t, skills)
	// Add ops-autopilot skill (L1+confirm so webhook passes trust gate)
	if err := os.WriteFile(filepath.Join(skills, "autopilot.yaml"), []byte(`
id: ops-autopilot
name: Ops Autopilot
description: autopilot
schedule: "*/15 * * * *"
trust_level: L1
scope: ops
timeout: 30
cron_actuation: confirm
mcp_tools: [get_cluster_summary, delete_pod]
prompt_template: autopilot
`), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandlerWithOptions(HandlerOptions{
		SkillsDir:  skills,
		StateDir:   t.TempDir(),
		Dispatcher: stubDispatcher{},
		Now:        func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) },
		SyncScan:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := testRouter(h)

	// POST /patrol/webhook/delivery-complete should trigger ops-autopilot
	body := bytes.NewBufferString(`{"reason":"pipeline bifrost-deliver-stg succeeded"}`)
	req := httptest.NewRequest(http.MethodPost, "/patrol/webhook/delivery-complete", body)
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook status %d %s", rec.Code, rec.Body.String())
	}
	var trig TriggerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &trig); err != nil {
		t.Fatal(err)
	}
	if trig.Status != StatusStarted || trig.Result != ResultRunning {
		t.Fatalf("webhook trigger: %+v", trig)
	}

	// Wait for async completion
	deadline := time.Now().Add(2 * time.Second)
	for {
		last := h.store.LastRun("ops-autopilot")
		if last != nil && last.Result == ResultSuccess {
			if last.Trigger != TriggerWebhook {
				t.Fatalf("expected trigger=webhook, got %s", last.Trigger)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for autopilot completion: %+v", last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHandlerWebhookUnknownEvent(t *testing.T) {
	h := testHandler(t)
	rt := testRouter(h)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/patrol/webhook/unknown-event", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown webhook event should be 400, got %d", rec.Code)
	}
}

func TestHandlerWebhookCustomSkillID(t *testing.T) {
	h := testHandler(t)
	rt := testRouter(h)

	body := bytes.NewBufferString(`{"skill_id":"fleet-drift-scan"}`)
	req := httptest.NewRequest(http.MethodPost, "/patrol/webhook/delivery-complete", body)
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("custom skill_id webhook status %d %s", rec.Code, rec.Body.String())
	}
	var trig TriggerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &trig); err != nil {
		t.Fatal(err)
	}
	// fleet-drift-scan is L0, syncScan=true, so it completes inline
	if trig.Status != StatusCompleted || trig.Result != ResultSuccess {
		t.Fatalf("custom skill_id webhook: %+v", trig)
	}
}

func TestHandlerTriggerInternal(t *testing.T) {
	h := testHandler(t)

	// TriggerInternal fires async
	h.TriggerInternal("fleet-drift-scan")

	deadline := time.Now().Add(2 * time.Second)
	for {
		last := h.store.LastRun("fleet-drift-scan")
		if last != nil && last.Result == ResultSuccess && last.Trigger == TriggerWebhook {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for TriggerInternal completion: %+v", last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHandlerTriggerInternalNilHandler(t *testing.T) {
	// Should not panic
	var h *Handler
	h.TriggerInternal("ops-autopilot")
}

func TestStoreUpdateRun(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendRun(PatrolRun{ID: "r1", SkillID: "x", Result: ResultRunning, StartedAt: "2026-08-09T01:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRun("r1", func(run *PatrolRun) {
		run.Result = ResultFailure
		run.Error = "HTTP_FAIL"
	}); err != nil {
		t.Fatal(err)
	}
	last := s.LastRun("x")
	if last == nil || last.Result != ResultFailure || last.Error != "HTTP_FAIL" {
		t.Fatalf("last=%+v", last)
	}
	if err := s.UpdateRun("missing", func(*PatrolRun) {}); err == nil {
		t.Fatal("expected missing run error")
	}
}

func TestStoreRingBufferCap(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxRuns+20; i++ {
		if err := s.AppendRun(PatrolRun{ID: "r", SkillID: "x", Result: ResultSuccess, StartedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
	}
	runs, total := s.ListRuns(500)
	if total != MaxRuns || len(runs) != MaxRuns {
		t.Fatalf("total=%d len=%d", total, len(runs))
	}
}
