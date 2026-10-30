package stgrelease

import (
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func archivedChild(t *testing.T, raw map[string]any, task string) *unstructured.Unstructured {
	t.Helper()
	for _, value := range raw["child_refs"].([]any) {
		ref := value.(map[string]any)
		if ref["pipelineTaskName"] == task {
			return &unstructured.Unstructured{Object: ref["task_run"].(map[string]any)}
		}
	}
	t.Fatalf("missing archived child %s", task)
	return nil
}

func TestControlFailedRunChecksArchivedInputs(t *testing.T) {
	t.Parallel()
	for _, task := range []string{"clone-platform", "clone-ui", "clone-infra"} {
		for _, missingPeer := range []bool{false, true} {
			name := task
			if missingPeer {
				name += "/missing_peer"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				c, l, r, _ := controlFixture(t, "Failed")
				pass(t, c)
				tr := l.state(t).Batches[leaseBatch].Tries[1]
				// Fold returns copies; mutate the stored event itself so the
				// next replay actually sees the unauthorized clone result.
				raw := l.entries[tr.RunEvidence.Seq-1].Event.Body["stg_release"].(map[string]any)["raw"].(map[string]any)
				child := archivedChild(t, raw, task)
				child.Object["status"].(map[string]any)["results"] = []any{map[string]any{"name": "commit", "value": sha("9")}}
				if missingPeer {
					peer := "clone-platform"
					if task == peer {
						peer = "clone-infra"
					}
					archivedChild(t, raw, peer).Object["status"].(map[string]any)["results"] = []any{}
				}
				// A restarted controller must use the durable archive, even
				// after Tekton cleanup makes the original run unreadable.
				r.observeErr = errors.New("run deleted")
				c = &Controller{Ledger: l, Runs: r, Policy: c.Policy, Clock: c.Clock, Cluster: c.Cluster}
				for range 3 {
					pass(t, c)
				}
				s := l.state(t)
				b := s.Batches[leaseBatch]
				tr = b.Tries[1]
				if tr.Verify.Record.String("why") != "wrong_input" || tr.Verify.Record.String("outcome") != "failed" || len(s.Blocks) != 1 || b.Phase != "CLOSED" || tr.Retry.Seq != 0 || tr.Effect.Seq != 0 {
					t.Fatalf("unauthorized clone retried: verify=%v effect=%v retry=%d phase=%s blocks=%d", tr.Verify.Record, tr.Effect.Record, tr.Retry.Seq, b.Phase, len(s.Blocks))
				}
				for _, block := range s.Blocks {
					if !block.IsOpen() || block.Opened.Record.String("why") != "wrong_input" {
						t.Fatal("missing persistent wrong_input interval")
					}
				}
				seq := s.LastSeq
				l.clock.Step(30 * time.Minute)
				for range 100 {
					pass(t, c)
				}
				s = l.state(t)
				if s.LastSeq != seq || s.LastBatch != 1 || len(s.BlockHistory) != 1 || r.creates != 0 || r.observations != 1 || r.workReads != 0 {
					t.Fatal("wrong_input replay changed its interval or attempted a retry")
				}
			})
		}
	}
}

func TestControlFailedRunMatchingInputsRetry(t *testing.T) {
	t.Parallel()
	for _, class := range []string{"unchanged", "this_try"} {
		t.Run(class, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Failed")
			// The clones succeeded, but subsequent build and pin tasks failed.
			// Retry eligibility must not require successful deployment evidence.
			for _, task := range []string{"build-platform-api", "gitops-sync"} {
				status := archivedChild(t, r.obs.Raw, task).Object["status"].(map[string]any)
				status["conditions"] = []any{map[string]any{"type": "Succeeded", "status": "False"}}
				status["results"] = []any{}
			}
			r.workloads = workloads(dig("a"), dig("c"))
			if class == "this_try" {
				r.workloads["platform-console"].(map[string]any)["digest"] = dig("f")
			}
			for range 4 {
				pass(t, c)
			}
			s := l.state(t)
			tr := s.Batches[leaseBatch].Tries[1]
			if tr.Verify.Record.String("why") != "run_failed" || tr.Effect.Record.String("class") != class || tr.Retry.Seq == 0 || len(s.Blocks) != 0 {
				t.Fatalf("matching failed run lost retry: verify=%v effect=%v retry=%d blocks=%d", tr.Verify.Record, tr.Effect.Record, tr.Retry.Seq, len(s.Blocks))
			}
			l.clock.Step(10 * time.Minute)
			pass(t, c)
			next := l.state(t).Batches[leaseBatch].Tries[2]
			if next == nil || expected(next) != expected(tr) || object(next.Snapshot.Record, "base").String("P") != sha("e") || r.creates != 0 {
				t.Fatal("retry changed frozen inputs or trusted baseline")
			}
			if s.Budget[store.StgBudgetKey{R: controlMember.R, Gen: 1}] != 1 {
				t.Fatal("input verification changed the debit")
			}
		})
	}
}

func TestControlTerminalUnknownInputsWait(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"Failed", "Succeeded"} {
		for _, mode := range []string{"no_children", "missing_commit", "invalid_commit", "failed_clone", "owner_uid", "wrong_repo", "wrong_task", "malformed_archive"} {
			t.Run(status+"/"+mode, func(t *testing.T) {
				t.Parallel()
				c, l, r, _ := controlFixture(t, status)
				child := archivedChild(t, r.obs.Raw, "clone-ui")
				switch mode {
				case "no_children":
					r.obs.Raw["child_refs"] = []any{}
				case "missing_commit":
					child.Object["status"].(map[string]any)["results"] = []any{}
				case "invalid_commit":
					child.Object["status"].(map[string]any)["results"] = []any{map[string]any{"name": "commit", "value": "invalid"}}
				case "failed_clone":
					child.Object["status"].(map[string]any)["conditions"] = []any{map[string]any{"type": "Succeeded", "status": "False"}}
				case "owner_uid":
					owners := child.GetOwnerReferences()
					owners[0].UID = "foreign"
					child.SetOwnerReferences(owners)
				case "wrong_repo":
					child.Object["spec"].(map[string]any)["params"] = []any{map[string]any{"name": "url", "value": "https://other/bifrost-ui.git"}}
				case "wrong_task":
					child.Object["spec"].(map[string]any)["taskRef"] = map[string]any{"name": "other"}
				case "malformed_archive":
					r.obs.Raw["child_refs"].([]any)[0].(map[string]any)["kind"] = "Foreign"
				}
				for range 4 {
					pass(t, c)
				}
				s := l.state(t)
				b := s.Batches[leaseBatch]
				tr := b.Tries[1]
				if tr.Verify.Seq != 0 || tr.Effect.Seq != 0 || tr.Retry.Seq != 0 || b.Phase != "TERMINAL" || len(s.Blocks) != 0 || len(b.Diagnostics) != 1 || b.Diagnostics["verify_unknown/1/1"].Seq == 0 || r.creates != 0 || r.workReads != 0 {
					t.Fatalf("unknown input became retryable: verify=%v effect=%v phase=%s diagnostics=%v", tr.Verify.Record, tr.Effect.Record, b.Phase, b.Diagnostics)
				}
			})
		}
	}
}
