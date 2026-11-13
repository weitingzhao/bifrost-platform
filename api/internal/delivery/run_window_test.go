package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRunWindowLifecycle(t *testing.T) {
	t.Parallel()
	var events []WindowEvent
	recorder := WindowRecorder(func(_ context.Context, e WindowEvent) { events = append(events, e) })
	ctx := windowContext(context.Background(), recorder)
	cs := fake.NewSimpleClientset()
	now := time.Now().UTC()
	first, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "prod-run", What: "bifrost-platform"}, now)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := &runWindow{cs: cs, ns: "cicd", who: "prod-run", version: cm.ResourceVersion, recorder: recorder}
	if err := w.update(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("run renewal emitted an event: %+v", events)
	}
	held, live, err := getReleaseWindow(ctx, cs, "cicd", now)
	if err != nil || !live || held.StartedAt != first.StartedAt {
		t.Fatalf("renew: %+v %v", held, err)
	}
	_, err = putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "another", What: "bifrost-platform"}, now)
	for _, detail := range []string{"prod-run", "bifrost-platform", first.StartedAt, held.ExpiresAt} {
		if err == nil || !strings.Contains(err.Error(), detail) {
			t.Fatalf("missing %s: %v", detail, err)
		}
	}
	if err := w.update(ctx, true); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[2].Action != "released" || events[2].Who != first.Who || events[2].StartedAt != first.StartedAt {
		t.Fatalf("run events: %+v", events)
	}
	if _, live, err := getReleaseWindow(ctx, cs, "cicd", time.Now().UTC()); err != nil || live {
		t.Fatalf("release: %v %v", live, err)
	}
}

func TestWindowTableAndRefusalDetails(t *testing.T) {
	t.Parallel()
	for pipeline, entry := range windowPipelines {
		repos, known := requiredRepos(pipeline)
		if !known || sortedKeys(repos) != sortedKeys(entry.repos) || mustHoldWindow(pipeline) != entry.required {
			t.Fatal(pipeline)
		}
	}
	window := map[string]any{"who": "owner", "what": "bifrost-platform", "started_at": "2026-01-01T00:00:00Z", "expires_at": "2099-01-01T00:00:00Z"}
	msg := decideReleaseWindow(true, window, "unlisted-build", "")
	for _, key := range []string{"who", "what", "started_at", "expires_at"} {
		if !strings.Contains(msg, window[key].(string)) {
			t.Fatal(msg)
		}
	}
}

func TestRunWindowTerminalConditions(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"Unknown", "True", "False"} {
		run := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}}}}
		if runEnded(run) != (status != "Unknown") {
			t.Fatal(status)
		}
	}
	if windowLifetime != time.Duration(windowTTLDefault)*time.Minute {
		t.Fatal("lease lifetime differs")
	}
}

func TestApprovedDeliverStartOwnsWindowAndRefusesForeignHolder(t *testing.T) {
	t.Parallel()
	const name = "bifrost-deliver-prod"
	pipe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": name, "namespace": "cicd"}, "spec": map[string]any{}}}
	s, dyn, _ := contractService(t, pipe, "unknown")
	cs := fake.NewSimpleClientset(readyAmd64Node())
	s.SetClientsetForTest(cs)
	ctx := actions.WithCreateAttempt(context.Background(), "approval-window", 1)
	resp, _, err := s.StartPipelineRun(ctx, name, factSHA1, "", "", nil)
	if err != nil || !resp.OK {
		t.Fatalf("start: %+v %v", resp, err)
	}
	run := createdContractRun(t, dyn)
	held, live, err := getReleaseWindow(ctx, cs, "cicd", time.Now().UTC())
	if err != nil || !live || held.Who != run.GetName() {
		t.Fatalf("run holder: %+v %v", held, err)
	}
	other := actions.WithCreateAttempt(context.Background(), "approval-other", 1)
	resp, _, err = s.StartPipelineRun(other, name, factSHA1, "", "", nil)
	if err == nil || !strings.Contains(resp.Message, run.GetName()) || !strings.Contains(resp.Message, held.StartedAt) || !strings.Contains(resp.Message, held.ExpiresAt) {
		t.Fatalf("foreign start: %+v %v", resp, err)
	}
	// The observer sees deletion as completion and ends its own lease.
	if err := dyn.Resource(pipelineRunGVR).Namespace("cicd").Delete(ctx, run.GetName(), metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunWindowObserverRenewsAndEnds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	now := time.Now().UTC()
	_, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "run", What: "bifrost-platform"}, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := &runWindow{cs: cs, ns: "cicd", who: "run", version: cm.ResourceVersion}
	run := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": map[string]any{"name": "run", "namespace": "cicd"}}}
	dyn := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), run)
	res := dyn.Resource(pipelineRunGVR).Namespace("cicd")
	ended, err := w.observe(ctx, res, "run")
	if err != nil || ended {
		t.Fatalf("running: %v %v", ended, err)
	}
	held, live, err := getReleaseWindow(ctx, cs, "cicd", now)
	if err != nil || !live {
		t.Fatalf("renew: %v %v", live, err)
	}
	expires, err := time.Parse(time.RFC3339, held.ExpiresAt)
	if err != nil || expires.Before(now.Add(windowLifetime-time.Second*2)) {
		t.Fatalf("not renewed: %s", held.ExpiresAt)
	}
	run.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}}}
	if _, err := res.Update(ctx, run, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ended, err = w.observe(ctx, res, "run")
	if err != nil || !ended {
		t.Fatalf("terminal: %v %v", ended, err)
	}
	if _, live, err := getReleaseWindow(ctx, cs, "cicd", time.Now().UTC()); err != nil || live {
		t.Fatalf("not released: %v %v", live, err)
	}
}

// #514: approval preserves the original request's who; it must not try to
// replace that requester's still-live session window with the run name.
func TestApprovedRequesterWindow514(t *testing.T) {
	ctx := context.Background()
	const name = "bifrost-deliver-prod"
	pipe := baselinePipelines(t)[name].DeepCopy()
	pipe.SetNamespace("cicd")
	s, dyn, _ := contractService(t, pipe, "unknown")
	cs := fake.NewSimpleClientset(readyAmd64Node())
	s.SetClientsetForTest(cs)
	before, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "biz-align", What: "bifrost-trade-core"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ctx = actions.WithCreateAttempt(ctx, "approval-514", 1)
	resp, _, err := s.StartPipelineRun(ctx, name, factSHA1, "", "biz-align", nil)
	if err != nil || !resp.OK {
		t.Fatalf("approved requester: %+v %v", resp, err)
	}
	createdContractRun(t, dyn)
	after, live, err := getReleaseWindow(ctx, cs, "cicd", time.Now().UTC())
	if err != nil || !live || after != before {
		t.Fatalf("session window changed: %+v %v", after, err)
	}
}
