package store_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's architecture fixture (console/src/api/__fixtures__/agentrt-progress-arch.testdb.json):
// GET /progress with an ARCH root of four rows, one of each kind the page shows (review 431 C2). Off
// unless asked for; it writes the file, and the Console's tests read it:
//
//	CONSOLE_ARCH_FIXTURE_WRITE=1 go test ./internal/agentrt/store/ -run TestConsoleArchFixture
const consoleArchFixture = "../../../../console/src/api/__fixtures__/agentrt-progress-arch.testdb.json"

func TestConsoleArchFixture(t *testing.T) {
	t.Parallel()
	if os.Getenv("CONSOLE_ARCH_FIXTURE_WRITE") != "1" {
		t.Skip("writes the Console's fixture only when asked")
	}
	w := newKernelWorld(t)
	rows := []struct{ id, title, rule string }{
		{"ARCH:state-db", "状态库（PostgreSQL agentrt 模式）", archGates("stg", "T19", "T20")},
		{"ARCH:bus", "redis-agents 总线", archGates("stg", "T21")},
		{"ARCH:gateway", "推理网关", ""},
		{"ARCH:prod-runtime", "运行时在正式环境", archGates("prod", "T19")},
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "ARCH", Title: "架构对照", Root: "ARCH"}); err != nil {
			return err
		}
		for i, r := range rows {
			if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: r.id, Parent: "ARCH", Kind: "arch_row", Label: r.id[5:], Title: r.title, Ordinal: i, Root: "ARCH"}); err != nil {
				return err
			}
		}
		return nil
	}))
	for _, r := range rows {
		if r.rule == "" {
			continue
		}
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("ARCH"), Type: "plan_changed", Actor: "controller",
				Body: map[string]any{"change": "set_done_rule"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, r.id, r.rule)
			return err
		}))
	}
	ref := map[string]any{"delivery_run": "bifrost-deliver-platform-1792461534", "images": map[string]any{"platform-api": "sha256:aa", "platform-workers": "sha256:aa"}}
	_, err := store.SeeDeploy(ctx, w.rt, "stg", deployedIs(commitA, ref))
	ok(t, err)
	task := store.NewTaskID()
	var attempt string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: task, MissionID: "M-1", PlanNode: s1, Role: "implement",
			Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "none"}); err != nil {
			return err
		}
		var err error
		attempt, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", task, "exec:deterministic", "", 1, time.Minute)
		return err
	}))
	for _, test := range []string{"T19", "T20"} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			ev, err := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte(test))
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, check_ref, env, verdict,
			  evidence_artifact_id, subject_commit, deploy_ref_json, spec_sha256) VALUES ($1,$2,$3,'gate',$4,'stg','pass',$5,$6,$7,$8)`,
				store.NewVerificationID(), task, attempt, test, ev, commitA, ref, archSpec)
			return err
		}))
	}
	_, err = store.PostPlanNote(ctx, w.rt, store.Writer{Actor: "thread:night", Token: "fixture-operator-1"}, "n1", "ARCH:state-db", "PROD 与 STG 都在用；PROD 重建演练还欠")
	ok(t, err)
	_, err = store.PostPlanNote(ctx, w.rt, owner, "n2", "ARCH:gateway", "还没有：模型直连厂商")
	ok(t, err)
	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.MarshalIndent(out, "", "  ")
	ok(t, err)
	ok(t, os.WriteFile(consoleArchFixture, append(raw, '\n'), 0o644))
}
