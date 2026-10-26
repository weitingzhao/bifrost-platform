package agentrtapi_test

// Copy into api/internal/agentrt/agentrtapi temporarily; run TestCapturePlanView
// with AGENTRT_TEST_PG pointing to a disposable local test PostgreSQL.
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestCapturePlanView(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	raw, err := os.ReadFile("../planview/testdata/r4-v1.json")
	must(t, err)
	doc, err := planview.Parse(raw)
	must(t, err)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R4", Root: "R4", Kind: "round", Label: "4", Title: "包工队自己接活、自己交房", Ordinal: 4})
		if err != nil {
			return err
		}
		for _, n := range doc.Items {
			parent := n.Parent
			kind, stepKind := "item", ""
			if parent != "R4" {
				parent = "R4:" + parent
				kind = "step"
				stepKind = "agent"
			}
			_, err = store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R4:" + n.Node, Root: "R4", Parent: parent, Kind: kind, StepKind: stepKind, Label: n.Node, Title: n.Title, Ordinal: n.Ordinal})
			if err != nil {
				return err
			}
		}
		for i, n := range []store.PlanNode{
			{ID: "R4:4.1-1", Parent: "R4:4.1", Label: "4.1-1", Title: "实现自动交房", StepKind: "agent", MissionID: "M-1"},
			{ID: "R4:4.1-2", Parent: "R4:4.1", Label: "4.1-2", Title: "验收真实交付链路", StepKind: "owner"},
		} {
			n.Root = "R4"
			n.Kind = "step"
			n.Ordinal = i + 1
			if _, err = store.CreatePlanNode(ctx, tx, "controller", n); err != nil {
				return err
			}
		}
		return nil
	}))

	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R3", Root: "R3", Kind: "round", Label: "3", Title: "空计划", Ordinal: 3})
		return err
	}))
	var artifact string
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		artifact, err = store.PutArtifact(ctx, tx, "controller", "other", "application/json", raw)
		return err
	}))
	zero := 0
	_, err = store.ProposePlanChange(ctx, pool, store.Writer{Actor: "thread:capture", Token: "fixture"}, "adopt", "R4", "fixture", []store.PlanChange{{Op: "adopt_plan_view", Artifact: artifact, ExpectedVersion: &zero}}, func(context.Context, store.Card) (string, error) { return "q_fixture", nil })
	must(t, err)
	_, err = store.DecidePlanChanges(ctx, pool, func(_ context.Context, id string) (store.Question, bool) {
		return store.Question{ID: id, Status: store.QuestionAnswered, Option: store.CardApplyKey, AnsweredBy: "owner", AnsweredAt: time.Now().UTC().Format(time.RFC3339Nano)}, true
	}, nil)
	must(t, err)
	mark := true
	_, err = store.ProposePlanChange(ctx, pool, store.Writer{Actor: "thread:capture", Token: "fixture"}, "marks", "R4", "fixture links", []store.PlanChange{{Op: "set_marks", Node: "R4:4.1-2", OwnerStep: &mark}}, func(context.Context, store.Card) (string, error) { return "q_fixture2", nil })
	must(t, err)
	_, err = store.DecidePlanChanges(ctx, pool, func(_ context.Context, id string) (store.Question, bool) {
		return store.Question{ID: id, Status: store.QuestionAnswered, Option: store.CardApplyKey, AnsweredBy: "owner", AnsweredAt: time.Now().UTC().Format(time.RFC3339Nano)}, true
	}, nil)
	must(t, err)
	code, claim := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}}})
	if code != 200 {
		t.Fatalf("claim %d %v", code, claim)
	}
	code, out := call(t, s, "agentd:h1", "POST", "attempts/"+claim["attempt_id"].(string)+"/start", map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"], "model_provider": "cursor-mix"})
	if code != 200 {
		t.Fatalf("start %d %v", code, out)
	}
	spec := []byte(`{"v":1,"kind":"agentrt.external_work/v1","task":"实现全计划与现场视图","executor":"codex","provider":"openai","repo":"platform","branch":"w31/pv4","acceptance_refs":["PV4"],"estimate_minutes":[10,20]}`)
	code, out = call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "other", session, "pv4")
	if code != 200 {
		t.Fatal(out)
	}
	code, out = call(t, s, "operator", "POST", "external-work", map[string]any{"idempotency_key": "pv4", "root": "R4", "plan_node_id": "R4:4.1-1", "source_run_key": "work624", "artifact_id": artifactID(spec)}, session, "pv4")
	if code != 200 {
		t.Fatal(out)
	}
	id := out["external_work_id"].(string)
	for seq := 1; seq <= 23; seq++ {
		typ := "heartbeat"
		if seq == 1 {
			typ = "started"
		}
		code, out = call(t, s, "operator", "POST", "external-work/"+id+"/events", map[string]any{"idempotency_key": string(rune('a' + seq)), "seq": seq, "type": typ}, session, "pv4")
		if code != 200 {
			t.Fatal(out)
		}
	}
	code, out = call(t, s, "operator", "POST", "external-work", map[string]any{"idempotency_key": "linked", "root": "R2", "plan_node_id": "R2:1-1", "task_id": task, "mission_id": "M-1", "source_run_key": "linked-runtime", "artifact_id": artifactID(spec)}, session, "pv4")
	if code != 200 {
		t.Fatal(out)
	}
	code, out = call(t, s, "operator", "POST", "external-work", map[string]any{"idempotency_key": "finished", "root": "R4", "plan_node_id": "R4:4.1-2", "source_run_key": "finished-work", "artifact_id": artifactID(spec)}, session, "pv4")
	if code != 200 {
		t.Fatal(out)
	}
	finishedID := out["external_work_id"].(string)
	for i, typ := range []string{"started", "submitted"} {
		code, out = call(t, s, "operator", "POST", "external-work/"+finishedID+"/events", map[string]any{"idempotency_key": typ, "seq": i + 1, "type": typ}, session, "pv4")
		if code != 200 {
			t.Fatal(out)
		}
	}
	code, out = call(t, s, "viewer", "GET", "plan-view?root=R4", nil)
	if code != 200 {
		t.Fatal(out)
	}
	if out["plan_version"] != float64(1) {
		t.Fatalf("adoption failed %v", out)
	}
	dir := "../../../../console/src/api/__fixtures__"
	save := func(name string, doc any) {
		b, e := json.MarshalIndent(doc, "", "  ")
		must(t, e)
		must(t, os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0644))
	}
	save("plan-view-handler.json", out)
	code, page := call(t, s, "viewer", "GET", "plan-view?root=R4&recent_cursor="+out["recent_next"].(string), nil)
	if code != 200 {
		t.Fatal(page)
	}
	save("plan-view-handler-page2.json", page)
	code, empty := call(t, s, "viewer", "GET", "plan-view?root=R2", nil)
	if code != 200 {
		t.Fatal(empty)
	}
	save("plan-view-handler-legacy.json", empty)
	code, empty = call(t, s, "viewer", "GET", "plan-view?root=R3", nil)
	if code != 200 {
		t.Fatal(empty)
	}
	save("plan-view-handler-empty.json", empty)
}
