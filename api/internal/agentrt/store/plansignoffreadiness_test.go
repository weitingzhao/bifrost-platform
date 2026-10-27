package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func pvSignoffReadiness(t *testing.T, w *world, step, want string, candidates bool) {
	t.Helper()
	before := w.footprint(t)
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	if before != w.footprint(t) {
		t.Fatal("reading readiness wrote state")
	}
	found := 0
	for _, bucket := range []string{"owner_waiting", "owner_later"} {
		for _, raw := range out[bucket].([]any) {
			entry := raw.(map[string]any)
			if entry["plan_node_id"] != step {
				continue
			}
			found++
			if entry["readiness"] != want || (bucket == "owner_waiting") != (want == "Ready") {
				t.Fatalf("%s: %v; want %s", bucket, entry, want)
			}
			encoded, err := json.Marshal(entry["candidate_evidence"])
			ok(t, err)
			var rows []json.RawMessage
			ok(t, json.Unmarshal(encoded, &rows))
			if rows == nil || (len(rows) > 0) != candidates {
				t.Fatalf("unexpected candidates: %s", encoded)
			}
		}
	}
	if found != 1 {
		t.Fatalf("signoff occurs %d times", found)
	}
}

func TestPV2SignoffReadinessRequiresWork(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "external_verified", "owner_signoff")
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	pvSignoffReadiness(t, w, s1, "Not started", false)
	w.ready(t, store.Task{}) // A task without an attempt is still unstarted.
	pvSignoffReadiness(t, w, s1, "Not started", false)
	who := wr("thread:signoff-readiness")
	reg, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "work-635", Artifact: externalSpec(t, w)})
	ok(t, err)
	id := reg["external_work_id"].(string)
	pvSignoffReadiness(t, w, s1, "Not started", false)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "submit", Seq: 2, Type: "submitted", ReportDigest: w.artifact(t, "report", "Candidate evidence")})
	ok(t, err)
	if w.stepState(t, s1) != "planned" {
		t.Fatal("candidate case must leave execution unstarted")
	}
	pvSignoffReadiness(t, w, s1, "Ready", true)
}

func TestPV2SignoffReadinessExecution(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"active", "done", "attempt", "other-step attempt"} {
		t.Run(mode, func(t *testing.T) {
			w := newKernelWorld(t)
			d := pvDocument(t, 1)
			b := pvBinding(d, s1, "external_verified", "owner_signoff")
			adoptView(t, w, pvArtifact(t, w, d, b), 0)
			if mode == "active" || mode == "done" {
				ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
					_, err := store.SetStepState(ctx, tx, "controller", root, s1, mode, "execution readiness test")
					return err
				}))
				pvSignoffReadiness(t, w, s1, "Ready", false)
				return
			}
			step, want := s1, "Ready"
			if mode == "other-step attempt" {
				step, want = s2, "Not started"
			}
			task := w.ready(t, store.Task{PlanNode: step})
			c := w.claim(t, "h1", time.Minute, codex)
			if c == nil || c.TaskID != task {
				t.Fatal("task was not claimed")
			}
			if w.stepState(t, s1) != "planned" {
				t.Fatal("attempt case must use an unstarted controller state")
			}
			pvSignoffReadiness(t, w, s1, want, false)
			_, err := store.Ended(ctx, w.rt, c.AttemptID, "h1", "daemon_restart", true)
			ok(t, err)
			// Readiness must retain an ended attempt, not just liveAttempts.
			pvSignoffReadiness(t, w, s1, want, false)
		})
	}
}

func TestPV2SignoffReadinessBlocked(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R2:2.2", Parent: root, Kind: "item", Label: "2.2", Title: "Prerequisite", Root: root, Ordinal: 2}); err != nil {
			return err
		}
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R2:2.2-1", Parent: "R2:2.2", Kind: "step", StepKind: "agent", Label: "2.2-1", Title: "Prerequisite step", Root: root})
		return err
	}))
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "external_verified", "owner_signoff")
	prerequisite := d.Items[0]
	prerequisite.Node, prerequisite.Title, prerequisite.Ordinal = "2.2", "Prerequisite", 2
	order := 2
	prerequisite.Order = &order
	pre := b
	pre.ID, pre.Node, pre.Step = "prerequisite", "2.2", "R2:2.2-1"
	pre.Atom, pre.Source, pre.Repo, pre.Result = "main_effective", "git", "bifrost-platform", commitA
	raw, err := json.Marshal(pre)
	ok(t, err)
	prerequisite.Bindings = []json.RawMessage{raw}
	d.Items = append(d.Items, prerequisite)
	d.Items[0].Dependencies = []planview.Edge{{Root: root, Node: "2.2", Milestone: "accepted", RequiredFor: "accepted", Relation: "blocks", Reason: "Prerequisite must pass", Sources: []string{"work"}}}
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	pvPoll(t, w, store.PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: landing.Never}, nil
	}})
	pvSignoffReadiness(t, w, s1, "Blocked", false)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.SetStepState(ctx, tx, "controller", root, s1, "active", "execution readiness test")
		return err
	}))
	pvSignoffReadiness(t, w, s1, "Blocked", false)
	who := wr("thread:blocked-readiness")
	reg, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "work-635", Artifact: externalSpec(t, w)})
	ok(t, err)
	id := reg["external_work_id"].(string)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "submit", Seq: 2, Type: "submitted", ReportDigest: w.artifact(t, "report", "Blocked candidate")})
	ok(t, err)
	pvSignoffReadiness(t, w, s1, "Blocked", true)
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	waiting := out["waiting_on"].([]map[string]any)
	if len(waiting) != 1 || waiting[0]["status"] != "Blocked" {
		t.Fatalf("dependency must be false, not unknown: %v", waiting)
	}
}
