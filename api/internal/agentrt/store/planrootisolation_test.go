package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const corruptRoot = "Rbad"
const corruptItem = "Rbad:2.1"
const corruptStep = "Rbad:2.1-1"

func pvSecondRoot(t *testing.T, w *world) (string, planview.Binding) {
	t.Helper()
	for _, n := range []store.PlanNode{
		{ID: corruptRoot, Root: corruptRoot, Kind: "round", Ordinal: 99, Label: "bad", Title: "Second root"},
		{ID: corruptItem, Root: corruptRoot, Parent: corruptRoot, Kind: "item", Label: "2.1", Title: "Second item"},
		{ID: corruptStep, Root: corruptRoot, Parent: corruptItem, Kind: "step", Label: "2.1-1", Title: "Second step", StepKind: "agent"},
	} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.CreatePlanNode(ctx, tx, "controller", n)
			return err
		}))
	}
	d := pvDocument(t, 1)
	d.Root, d.Items[0].Parent = corruptRoot, corruptRoot
	b := pvBinding(d, corruptStep, "main_effective", "git")
	b.Root, b.Repo, b.Result = corruptRoot, "bifrost-platform", commitA
	artifact := pvArtifact(t, w, d, b)
	zero := 0
	a := &asker{}
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), "second-root", corruptRoot, "isolation fixture", []store.PlanChange{{Op: "adopt_plan_view", Artifact: artifact, ExpectedVersion: &zero}}, a.ask)
	ok(t, err)
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	_, err = store.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	return artifact, b
}

// Inject an unreadable stored document using a well-formed adoption event.
// Neither immutable blobs nor event guards are disabled by the test.
func pvCorruptView(t *testing.T, w *world, body []byte) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		artifact, err := store.PutArtifact(ctx, tx, "controller", "other", "application/json", body)
		if err != nil {
			return err
		}
		var raw []byte
		if err = tx.QueryRow(ctx, `SELECT body_json->'plan_view' FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'plan_view' ORDER BY seq DESC LIMIT 1`, store.PlanStream(corruptRoot)).Scan(&raw); err != nil {
			return err
		}
		parsed, err := canon.Parse(raw)
		if err != nil {
			return err
		}
		adoption := parsed.(map[string]any)
		adoption["artifact_id"] = artifact
		_, err = store.Append(ctx, tx, store.Event{Stream: store.PlanStream(corruptRoot), Type: "note", Actor: "controller", Body: map[string]any{"text": "Corrupt document fixture", "plan_view": adoption}})
		return err
	}))
}

func TestPV2CorruptViewIsolatesRoot(t *testing.T) {
	t.Parallel()
	for _, corruption := range []string{"schema", "json", "bindings", "encoding"} {
		t.Run(corruption, func(t *testing.T) {
			w := newKernelWorld(t)
			_, badBinding := pvSecondRoot(t, w)
			d := pvDocument(t, 1)
			goodBinding := pvBinding(d, s1, "main_effective", "git")
			goodBinding.Repo, goodBinding.Result = "bifrost-platform", commitA
			adoptView(t, w, pvArtifact(t, w, d, goodBinding), 0)
			src := store.PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
				return landing.MainHistory{State: landing.Effective, Commit: commitA}, nil
			}}
			pvPoll(t, w, src)
			pvEvaluate(t, w, s1, "done")
			pvEvaluate(t, w, corruptStep, "done")
			before, err := store.PlanView(ctx, w.rt, root)
			ok(t, err)
			body := []byte(`{"items":{}}`)
			switch corruption {
			case "json":
				body = []byte(`{"items":`)
			case "bindings":
				body = []byte(`{"items":[{"bindings":[{"attempt":"invalid","round":"not-an-integer"}]}]}`)
			case "encoding":
				body = []byte{0xff, 0xfe}
			}
			pvCorruptView(t, w, body)
			// The collector and rule evaluator share readPlanFacts with PlanView.
			// A prior true observation for the corrupt root cannot keep it done.
			pvPoll(t, w, src)
			pvEvaluate(t, w, s1, "done")
			pvEvaluate(t, w, corruptStep, "active")
			after, err := store.PlanView(ctx, w.rt, root)
			ok(t, err)
			for _, field := range []string{"items", "edges", "mainline", "next", "waiting_on", "owner_waiting", "owner_later", "warnings"} {
				old, _ := json.Marshal(before[field])
				got, _ := json.Marshal(after[field])
				if string(old) != string(got) {
					t.Fatalf("unrelated root changed %s: %s -> %s", field, old, got)
				}
			}
			bad, err := store.PlanView(ctx, w.rt, corruptRoot)
			ok(t, err)
			warnings := bad["warnings"].([]string) // Console's existing string[] wire shape.
			if len(warnings) == 0 || !strings.Contains(warnings[0], corruptRoot) || !strings.Contains(warnings[0], "view document unavailable:") || len([]rune(warnings[0])) > 240 {
				t.Fatalf("missing concise root/parse warning: %v", warnings)
			}
			if len(bad["items"].([]planview.Item)) != 0 || bad["freshness"].(map[string]any)["verification"] != "Unknown" || bad["plan_version"] != 1 {
				t.Fatalf("corrupt root was not retained as Unknown: %v", bad)
			}
			var persisted string
			ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'plan_observation'->>'value' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'plan_observation'->>'binding_sha256'=$2 ORDER BY seq DESC LIMIT 1`, "plan-observation:"+corruptRoot, badBinding.Digest()).Scan(&persisted))
			if persisted != "true" {
				t.Fatalf("historical evidence was overwritten: %s", persisted)
			}
		})
	}
}

func TestPV2CorruptDependencyKeepsGoodRootView(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pvSecondRoot(t, w)
	d := pvDocument(t, 1)
	d.Items[0].Dependencies = []planview.Edge{{Root: corruptRoot, Node: "2.1", Milestone: "accepted", RequiredFor: "accepted", Relation: "blocks", Reason: "Needs second root", Sources: d.Items[0].Acceptance[0].Sources}}
	b := pvBinding(d, s1, "main_effective", "git")
	b.Repo, b.Result = "bifrost-platform", commitA
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	src := store.PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: landing.Effective, Commit: commitA}, nil
	}}
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "done")
	pvCorruptView(t, w, []byte(`{"items":{}}`))
	pvEvaluate(t, w, s1, "active")
	view, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	waiting := view["waiting_on"].([]map[string]any)
	if len(view["items"].([]planview.Item)) != 1 || len(view["edges"].([]planview.Dependency)) != 1 || len(waiting) != 1 || waiting[0]["status"] != "Unknown" {
		t.Fatalf("dependent view lost its structure: %v", view)
	}
	if got := view["mainline"].(map[string]any)["current"]; got == nil || !strings.Contains(fmt.Sprint(got), "unknown") {
		t.Fatalf("missing dependency was counted as true: %v", got)
	}
}
