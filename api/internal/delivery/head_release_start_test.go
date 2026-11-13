package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

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
			s, dyn, _ := contractService(t, pipe, "unknown")
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
				if err != nil || heldBy != "session" || run.Name != "" || len(told) != 0 {
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
