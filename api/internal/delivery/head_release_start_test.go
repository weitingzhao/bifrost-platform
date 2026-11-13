package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// 2026-11-13 15:45-15:58 Chicago (ci-feedback 2, 8): a commit waited thirteen
// minutes while two lines held the release window in turn. The platform's own
// loop must not be a third contender: with a live window it starts nothing
// and names the holder; with none, or with the expired record every release
// leaves behind, it starts and the run holds the window itself.
func TestStartDeliveryIfFree(t *testing.T) {
	const name = "bifrost-deliver-platform"
	for _, state := range []string{"absent", "expired", "held"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			pipe := baselinePipelines(t)[name].DeepCopy()
			pipe.SetNamespace("cicd")
			s, _, _ := contractService(t, pipe, "unknown")
			dyn := listingDynamic(s, pipe)
			cs := fake.NewSimpleClientset(readyAmd64Node())
			s.SetClientsetForTest(cs)
			if state != "absent" {
				when := time.Now().UTC()
				if state == "expired" {
					when = when.Add(-2 * windowLifetime)
				}
				if _, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "session", What: sortedKeys(windowPipelines[name].repos)}, when); err != nil {
					t.Fatal(err)
				}
			}
			var told []string
			h := NewHandlerForTest(s, nil)
			h.BindPipelineStartedHook(func(pipeline, revision, run, by, _ string) {
				told = append(told, pipeline+" "+revision+" "+run+" "+by)
			})
			run, heldBy, err := h.StartDeliveryIfFree(ctx, "head-release", name, factSHA1, map[string]string{"uiRevision": strings.Repeat("b", 40)})
			if state == "held" {
				if err != nil || heldBy != "the release window is held by session" || run.Name != "" || len(told) != 0 {
					t.Fatalf("held: run %+v heldBy %q told %v err %v", run, heldBy, told, err)
				}
				assertNoContractCreate(t, dyn)
				return
			}
			if err != nil || heldBy != "" || run.Name == "" {
				t.Fatalf("start: run %+v heldBy %q err %v", run, heldBy, err)
			}
			created := createdContractRun(t, dyn)
			held, live, err := getReleaseWindow(ctx, cs, "cicd", time.Now().UTC())
			if err != nil || !live || held.Who != created.GetName() {
				t.Fatalf("the run does not hold the window: %+v live=%v %v", held, live, err)
			}
			if len(told) != 1 || !strings.HasSuffix(told[0], " head-release") {
				t.Fatalf("the started-run hook: %v", told)
			}
		})
	}
}

// Codex review 2026-11-13, A1 (reproduced there on the real service with a
// fake cluster: "both live: automatic=... manual=..."). A run's window was
// not renewed and expired while the run still builds. The loop's start looks
// at the runs as well as the window and starts nothing beside it.
func TestStartDeliveryIfFreeStartsNothingBesideAnUnfinishedRun(t *testing.T) {
	const name = "bifrost-deliver-platform"
	ctx := context.Background()
	pipe := baselinePipelines(t)[name].DeepCopy()
	pipe.SetNamespace("cicd")
	s, _, _ := contractService(t, pipe, "unknown")
	dyn := listingDynamic(s, pipe)
	cs := fake.NewSimpleClientset(readyAmd64Node())
	s.SetClientsetForTest(cs)
	h := NewHandlerForTest(s, nil)
	first, heldBy, err := h.StartDeliveryIfFree(ctx, "head-release", name, factSHA1, map[string]string{"uiRevision": strings.Repeat("b", 40)})
	if err != nil || heldBy != "" || first.Name == "" {
		t.Fatalf("first: %+v %q %v", first, heldBy, err)
	}
	// Its window is long expired; the run has no verdict yet.
	if _, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: first.Name, What: sortedKeys(windowPipelines[name].repos)}, time.Now().UTC().Add(-2*windowLifetime)); err != nil {
		t.Fatal(err)
	}
	second, heldBy, err := h.StartDeliveryIfFree(ctx, "head-release", name, strings.Repeat("c", 40), map[string]string{"uiRevision": strings.Repeat("b", 40)})
	if err != nil || second.Name != "" || !strings.Contains(heldBy, first.Name) {
		t.Fatalf("second beside an unfinished run: %+v %q %v", second, heldBy, err)
	}
	runs, err := dyn.Resource(pipelineRunGVR).Namespace("cicd").List(ctx, metav1.ListOptions{})
	if err != nil || len(runs.Items) != 1 {
		t.Fatalf("%d runs, %v", len(runs.Items), err)
	}
}

// listingDynamic gives the service a fake cluster that can list runs (the
// start lists them), holding pipe.
func listingDynamic(s *Service, pipe *unstructured.Unstructured) *dynfake.FakeDynamicClient {
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"}, pipe)
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	return dyn
}
