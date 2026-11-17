package controller_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// setup is a migrated, seeded database with mission M-1, round R2, item
// R2:1 and steps R2:1-1 and R2:1-2 of M-1.
func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := agentrttest.Migrated(t)
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	must(t, err)
	pool := db.Pool(t, db.Runtime)
	_, err = pool.Exec(ctx, agentrttest.DispatchRules)
	must(t, err)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := storekit.CreateMission(ctx, tx, "controller", storekit.Mission{ID: "M-1", Title: "m", Goal: "g", Deadline: time.Now().Add(time.Hour)})
		return err
	}))
	for _, n := range []store.PlanNode{
		{ID: "R2", Kind: "round", Label: "2", Title: "round", Root: "R2"},
		{ID: "R2:1", Parent: "R2", Kind: "item", Label: "1", Title: "item", Root: "R2"},
		{ID: "R2:1-1", Parent: "R2:1", Kind: "step", Label: "1-1", Title: "one", MissionID: "M-1", StepKind: "agent", Root: "R2"},
		{ID: "R2:1-2", Parent: "R2:1", Kind: "step", Label: "1-2", Title: "two", MissionID: "M-1", StepKind: "agent", Ordinal: 1, Root: "R2"},
	} {
		must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.CreatePlanNode(ctx, tx, "controller", n)
			return err
		}))
	}
	return pool
}

func setRule(t *testing.T, pool *pgxpool.Pool, step, rule string, binds ...[2]string) {
	t.Helper()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("R2"), Type: "plan_changed", Actor: "controller",
			Body: map[string]any{"change": "test"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, step, rule); err != nil {
			return err
		}
		for _, b := range binds {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.plan_node_binding (plan_node_id, bound_kind, bound_ref, event_id)
			  VALUES ($1, $2, $3, current_setting('agentrt.event_id')::bigint)`, step, b[0], b[1]); err != nil {
				return err
			}
		}
		return nil
	}))
}

func stepState(t *testing.T, pool *pgxpool.Pool, step string) string {
	t.Helper()
	var s string
	must(t, pool.QueryRow(ctx, `SELECT state FROM agentrt.plan_node WHERE plan_node_id = $1`, step).Scan(&s))
	return s
}

// world is the outside the controller reads: approvals, questions, CI runs
// and deployments. A key that is absent cannot be read.
type world struct {
	mu        sync.Mutex
	approvals map[int64]string
	questions map[string]store.Question
	ci        map[string]string
	deploys   map[string]string
	reads     int
	panics    int // the next reads that panic
}

func (w *world) read() {
	w.mu.Lock()
	w.reads++
	p := w.panics > 0
	if p {
		w.panics--
	}
	w.mu.Unlock()
	if p {
		panic("source failed")
	}
}

func (w *world) sources() controller.Sources {
	return controller.Sources{
		Approval: func(_ context.Context, n int64) (store.ApprovalFact, bool) {
			w.read()
			w.mu.Lock()
			defer w.mu.Unlock()
			s, ok := w.approvals[n]
			return store.ApprovalFact{Number: n, Status: s, Action: "test", Tier: "test"}, ok
		},
		Question: func(_ context.Context, id string) (store.Question, bool) {
			w.read()
			w.mu.Lock()
			defer w.mu.Unlock()
			q, ok := w.questions[id]
			return q, ok
		},
		CI: func(_ context.Context, repo, commit string) (string, string, bool) {
			w.read()
			w.mu.Lock()
			defer w.mu.Unlock()
			s, ok := w.ci[repo+"@"+commit]
			return s, "ci-1", ok
		},
		Deployed: func(_ context.Context, env string) (string, map[string]any, bool) {
			w.read()
			w.mu.Lock()
			defer w.mu.Unlock()
			c, ok := w.deploys[env]
			if !ok {
				return "", nil, false
			}
			return c, map[string]any{"run": "deliver-" + c[:7]}, true
		},
	}
}

// Start runs the loops on timers; a pass that panics costs that pass only;
// cancelling the context stops them.
func TestStart(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-2", `{"approval_executed":20}`, [2]string{"approval", "20"})
	w := &world{approvals: map[int64]string{20: store.ApprovalExecuted}, panics: 1}
	var mu sync.Mutex
	var logs []string
	c := &controller.Controller{DB: pool, Sources: w.sources(), LeaseEvery: 20 * time.Millisecond, FactsEvery: 20 * time.Millisecond,
		Logf: func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, fmt.Sprintf(format, args...))
		}}
	run, cancel := context.WithCancel(ctx)
	wait := c.Start(run)
	deadline := time.Now().Add(10 * time.Second)
	for stepState(t, pool, "R2:1-2") != "done" {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("the loops did not move the step")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	stopped := make(chan struct{})
	go func() {
		wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the loops did not stop after cancel")
	}
	mu.Lock()
	panicked := strings.Contains(strings.Join(logs, "\n"), "panicked")
	mu.Unlock()
	if !panicked {
		t.Fatalf("the panicking pass was not reported: %v", logs)
	}
	time.Sleep(100 * time.Millisecond)
	w.mu.Lock()
	before := w.reads
	w.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	w.mu.Lock()
	after := w.reads
	w.mu.Unlock()
	if after != before {
		t.Fatalf("the loops still read after cancel: %d -> %d", before, after)
	}
}

// Review 152 B2: a source that does not answer is cut off after the read
// timeout; the pass goes on and records nothing for it.
func TestSlowSourceIsCutOff(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-1", `{"approval_executed":16}`, [2]string{"approval", "16"})
	c := &controller.Controller{DB: pool, ReadTimeout: 100 * time.Millisecond, Sources: controller.Sources{
		Approval: func(ctx context.Context, _ int64) (store.ApprovalFact, bool) {
			<-ctx.Done()
			return store.ApprovalFact{}, false
		},
	}}
	start := time.Now()
	n, err := c.Facts(ctx)
	must(t, err)
	if n != 0 || time.Since(start) > 5*time.Second {
		t.Fatalf("recorded %d after %v", n, time.Since(start))
	}
}

// Review 153 B1: a done rule's read of what is deployed, made while the step
// is held, is cut off after the read timeout too; the gate is unknown and
// nothing moves.
func TestSlowDeploymentReadInRulesIsCutOff(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-1", `{"gates_passed":[{"test":"T19","spec_sha256":"`+strings.Repeat("ab", 32)+`"}],"env":"stg"}`)
	c := &controller.Controller{DB: pool, ReadTimeout: 100 * time.Millisecond, Sources: controller.Sources{
		Deployed: func(ctx context.Context, _ string) (string, map[string]any, bool) {
			<-ctx.Done()
			return "", nil, false
		},
	}}
	start := time.Now()
	moved, err := c.Rules(ctx)
	must(t, err)
	if moved != 0 || time.Since(start) > 5*time.Second {
		t.Fatalf("moved %d after %v", moved, time.Since(start))
	}
}

// The loops decide plan changes: once the Owner answers a card with the
// first option, the change lands.
func TestStartDecidesPlanChanges(t *testing.T) {
	pool := setup(t)
	out, err := store.ProposePlanChange(ctx, pool, store.Writer{Actor: "thread:s1", Token: "operator"}, "k1", "R2", "a new item",
		[]store.PlanChange{{Op: "add_node", Parent: "R2", Kind: "item", Title: "second"}},
		func(context.Context, store.Card) (string, error) { return "q_9", nil })
	must(t, err)
	if out["question_id"] != "q_9" {
		t.Fatalf("proposal: %v", out)
	}
	w := &world{questions: map[string]store.Question{"q_9": {ID: "q_9", Status: store.QuestionAnswered, Option: store.CardApplyKey,
		AnsweredBy: "owner", AnsweredAt: "2026-10-14T20:00:00Z"}}}
	c := &controller.Controller{DB: pool, Sources: w.sources(), LeaseEvery: time.Hour, FactsEvery: 20 * time.Millisecond}
	run, cancel := context.WithCancel(ctx)
	wait := c.Start(run)
	defer wait()
	defer cancel()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var n int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.1'`).Scan(&n))
		if n == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the loops did not apply the answered change")
		}
	}
}
