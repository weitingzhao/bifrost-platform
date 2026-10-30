package stgrelease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestControlSupersededConsumesEachExistingFact(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"trusted", "no_evidence", "no_member_proof"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Succeeded")
			pass(t, c)
			l.clock.Step(16 * time.Minute)
			r.workloads = workloads(dig("8"), dig("9"))
			if mode != "no_evidence" {
				l.add(t, store.StgRecord{"v": "1", "scope": "ext", "kind": "evidence", "evidence": "cluster/cicd/other-uid", "source": "external", "run": "other-run", "P": sha("9"), "U": sha("b"), "infra": sha("c"), "content": map[string]any{"api_digest": dig("8"), "console_digest": dig("9")}, "deployed": map[string]any{"pin_sync_ok": true, "workloads": r.workloads, "at": l.clock.Now().Format(time.RFC3339Nano)}, "health": map[string]any{"status": "ok"}})
			}
			if mode == "trusted" {
				l.effective(t, controlMember, sha("9"))
			}
			l.clock.Step(time.Second)
			pass(t, c)
			if mode == "no_evidence" {
				if tr := l.state(t).Batches[leaseBatch].Tries[1]; tr.Verify.Record.String("why") != "rollout" {
					t.Fatal("digest equality invented provenance", tr.Verify)
				}
				return
			}
			pass(t, c)
			s := l.state(t)
			want := "released"
			if mode == "trusted" {
				want = "covered"
			}
			if s.Members[controlMember].To != want || s.Batches[leaseBatch].Phase != "CLOSED" || s.Batches[leaseBatch].Tries[1].Retry.Seq != 0 {
				t.Fatal("superseded member not independently settled", s.Members)
			}
			if mode == "trusted" && len(s.Covers) != 1 {
				t.Fatal("existing run evidence did not gain the missing cover")
			}
			seq := s.LastSeq
			pass(t, c)
			if l.state(t).LastSeq != seq {
				t.Fatal("replay duplicated coverage")
			}
		})
	}
}

func TestControlVerifiedMembersSettleIndependently(t *testing.T) {
	t.Parallel()
	c, l, _, _ := controlFixture(t, "Succeeded")
	other := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAW", Op: "land:two:1", R: sha("b")}
	for _, e := range l.entries {
		r, ok := e.Event.Body["stg_release"].(map[string]any)
		if !ok {
			continue
		}
		switch r["kind"] {
		case "opened":
			m := memberValue(other)
			m["gen"] = 1
			r["members"] = append(r["members"].([]any), m)
			r["first_pending_at"].(map[string]any)[other.R] = l.clock.Now().Format(time.RFC3339Nano)
		case "snapshot":
			r["set"] = append(r["set"].([]any), memberValue(other))
		case "started":
			r["spend"] = append(r["spend"].([]any), map[string]any{"R": other.R, "gen": 1, "n": 1})
		}
	}
	l.effective(t, controlMember, sha("a"))
	pass(t, c)
	pass(t, c)
	pass(t, c)
	s := l.state(t)
	if s.Members[controlMember].To != "delivered" || s.Members[other].To != "" || s.Batches[leaseBatch].Phase == "CLOSED" {
		t.Fatal("one unknown member changed the other member's outcome")
	}
	seq := s.LastSeq
	pass(t, c)
	if l.state(t).LastSeq != seq {
		t.Fatal("unknown validity generated a disposition")
	}
	l.effective(t, other, sha("a"))
	pass(t, c)
	if s = l.state(t); s.Members[other].To != "delivered" || s.Batches[leaseBatch].Phase != "CLOSED" {
		t.Fatal("new member proof did not settle")
	}
}

func TestControlExpiredRuleObservesButNeverSends(t *testing.T) {
	t.Parallel()
	for _, started := range []bool{true, false} {
		t.Run(map[bool]string{true: "started", false: "snapshot"}[started], func(t *testing.T) {
			t.Parallel()
			c, l, r, w := controlFixture(t, "Succeeded")
			p := c.Policy()
			rule := *p.Rule
			rule.Expires = l.clock.Now().Add(-time.Second)
			p.Rule = &rule
			c.Policy = func() WindowPolicy { return p }
			if !started {
				l.entries = l.entries[:5]
			}
			pass(t, c)
			s := l.state(t)
			if started {
				if s.Batches[leaseBatch].Tries[1].RunEvidence.Seq == 0 {
					t.Fatal("rule expiry stopped terminal observation")
				}
			} else if len(s.Blocks) != 1 || s.Batches[leaseBatch].Phase != "CLOSED" {
				t.Fatal("expired snapshot not blocked")
			}
			_, calls := w.snapshot()
			if r.creates != 0 || calls != [4]int{} {
				t.Fatal("expired rule touched send/window", calls)
			}
		})
	}
}

func TestControlFinalizerThirtyDaysDoesNotReopenSettledBatch(t *testing.T) {
	t.Parallel()
	c, l, r, _ := controlFixture(t, "Succeeded")
	l.effective(t, controlMember, sha("a"))
	pass(t, c)
	r.cleanErr = errors.New("patch forbidden")
	// Verification and settlement must still succeed despite cleanup failures.
	_ = c.Pass(context.Background())
	_ = c.Pass(context.Background())
	if phase(t, l) != "CLOSED" {
		t.Fatal("cleanup held settlement")
	}
	l.clock.Step(29 * 24 * time.Hour)
	_ = c.Pass(context.Background())
	if len(l.state(t).Batches[leaseBatch].Diagnostics) != 0 {
		t.Fatal("early finalizer alert")
	}
	l.clock.Step(24 * time.Hour)
	_ = c.Pass(context.Background())
	s := l.state(t)
	if len(s.Batches[leaseBatch].Diagnostics) != 1 || s.Batches[leaseBatch].Phase != "CLOSED" {
		t.Fatal("missing deduplicated finalizer alert")
	}
	seq := s.LastSeq
	_ = c.Pass(context.Background())
	if l.state(t).LastSeq != seq {
		t.Fatal("duplicate finalizer alert")
	}
	r.cleanErr = nil
	pass(t, c)
}

func TestControlArchiveRevalidatesChildIdentityAndBuilds(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"valid", "owner_uid", "duplicate_original", "original_uid", "failed_build", "malformed_archive"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Failed")
			var embedded map[string]any
			for _, v := range r.obs.Raw["child_refs"].([]any) {
				ref := v.(map[string]any)
				if x, ok := ref["pipeline_run"].(map[string]any); ok {
					embedded = x
				}
				if ref["pipelineTaskName"] != "build-platform-api" {
					continue
				}
				child := &unstructured.Unstructured{Object: ref["task_run"].(map[string]any)}
				switch mode {
				case "owner_uid":
					owners := child.GetOwnerReferences()
					owners[0].UID = "foreign"
					child.SetOwnerReferences(owners)
				case "failed_build":
					_ = unstructured.SetNestedSlice(child.Object, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
				}
			}
			refs, _, _ := unstructured.NestedSlice(embedded, "status", "childReferences")
			switch mode {
			case "duplicate_original":
				refs[len(refs)-1] = refs[0]
				_ = unstructured.SetNestedSlice(embedded, refs, "status", "childReferences")
			case "original_uid":
				refs[0].(map[string]any)["uid"] = "foreign"
				_ = unstructured.SetNestedSlice(embedded, refs, "status", "childReferences")
			case "malformed_archive":
				r.obs.Raw["child_refs"].([]any)[len(refs)-1].(map[string]any)["kind"] = "Foreign"
			}
			restored, err := delivery.RestoreSTGRun(r.obs.Identity, "Failed", r.obs.Raw)
			if mode == "valid" || mode == "failed_build" {
				if err != nil {
					t.Fatal(err)
				}
				api, console := restored.BuildDigests()
				want := dig("d")
				if mode == "failed_build" {
					want = ""
				}
				if api != want || console != dig("f") {
					t.Fatal("wrong independent build digests", api, console)
				}
			} else if err == nil {
				t.Fatal("invalid archive accepted", mode)
			}
			if mode == "malformed_archive" {
				pass(t, c)
				pass(t, c)
				pass(t, c)
				b := l.state(t).Batches[leaseBatch]
				tr := b.Tries[1]
				if tr.Verify.Seq != 0 || tr.Effect.Seq != 0 || tr.Retry.Seq != 0 || b.Diagnostics["verify_unknown/1/1"].Seq == 0 {
					t.Fatal("partial failed restore authorized verification or effects")
				}
			}
		})
	}
}

func TestControlInputUnblockNeedsANewRecordedHead(t *testing.T) {
	t.Parallel()
	c, l, _, _ := controlFixture(t, "Succeeded")
	l.entries = l.entries[:3]
	s := l.state(t)
	base, _ := store.StgTrustedBaseline(s)
	if err := c.blockAndFinish(context.Background(), s, leaseBatch, s.Batches[leaseBatch], "baseline_moved", base, nil); err != nil {
		t.Fatal(err)
	}
	l.clock.Step(time.Minute)
	l.effective(t, controlMember, sha("a"))
	pass(t, c)
	s = l.state(t)
	for _, b := range s.Blocks {
		if !b.IsOpen() {
			t.Fatal("same main head unblocked")
		}
	}
	l.clock.Step(time.Minute)
	l.effective(t, controlMember, sha("9"))
	pass(t, c)
	for _, b := range l.state(t).Blocks {
		if b.IsOpen() {
			t.Fatal("new recorded main did not unblock")
		}
	}
}
