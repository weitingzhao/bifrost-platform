package planloop_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planloop"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
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
	_, err := actor.Seed(ctx, db.Connect(t, db.Owner))
	must(t, err)
	pool := db.Pool(t, db.Runtime)
	_, err = pool.Exec(ctx, agentrttest.DispatchRules)
	must(t, err)
	must(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := storekit.CreateMission(ctx, tx, "controller", storekit.Mission{ID: "M-1", Title: "m", Goal: "g", Deadline: time.Now().Add(time.Hour)})
		return err
	}))
	for _, n := range []planstore.PlanNode{
		{ID: "R2", Kind: "round", Label: "2", Title: "round", Root: "R2"},
		{ID: "R2:1", Parent: "R2", Kind: "item", Label: "1", Title: "item", Root: "R2"},
		{ID: "R2:1-1", Parent: "R2:1", Kind: "step", Label: "1-1", Title: "one", MissionID: "M-1", StepKind: "agent", Root: "R2"},
		{ID: "R2:1-2", Parent: "R2:1", Kind: "step", Label: "1-2", Title: "two", MissionID: "M-1", StepKind: "agent", Ordinal: 1, Root: "R2"},
	} {
		must(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := planstore.CreatePlanNode(ctx, tx, "controller", n)
			return err
		}))
	}
	return pool
}

func setRule(t *testing.T, pool *pgxpool.Pool, step, rule string, binds ...[2]string) {
	t.Helper()
	must(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream("R2"), Type: "plan_changed", Actor: "controller",
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

// world is the outside the plan passes read: approvals, questions, CI runs
// and deployments. A key that is absent cannot be read.
type world struct {
	mu        sync.Mutex
	approvals map[int64]string
	questions map[string]seenfacts.Question
	ci        map[string]string
	deploys   map[string]string
}

func (w *world) sources() planloop.Sources {
	return planloop.Sources{
		Approval: func(_ context.Context, n int64) (seenfacts.ApprovalFact, bool) {
			w.mu.Lock()
			defer w.mu.Unlock()
			s, ok := w.approvals[n]
			return seenfacts.ApprovalFact{Number: n, Status: s, Action: "test", Tier: "test"}, ok
		},
		Question: func(_ context.Context, id string) (seenfacts.Question, bool) {
			w.mu.Lock()
			defer w.mu.Unlock()
			q, ok := w.questions[id]
			return q, ok
		},
		CI: func(_ context.Context, repo, commit string) (string, string, bool) {
			w.mu.Lock()
			defer w.mu.Unlock()
			s, ok := w.ci[repo+"@"+commit]
			return s, "ci-1", ok
		},
		Deployed: func(_ context.Context, env string) (string, map[string]any, bool) {
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

// A step whose rule's approval is executed moves to done: the facts pass
// records the approval, the rules pass evaluates the step.
//
// Origin: 2026-11-19, the controller split. The controller's TestStart moved
// this step through its timers; the passes are planloop's now and the
// timers stay the controller's, so the plan half of that test is here.
func TestFactsThenRulesMoveAStep(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-2", `{"approval_executed":20}`, [2]string{"approval", "20"})
	w := &world{approvals: map[int64]string{20: seenfacts.ApprovalExecuted}}
	l := &planloop.Loop{DB: pool, Sources: w.sources()}
	n, err := l.Facts(ctx)
	must(t, err)
	if n != 1 {
		t.Fatalf("recorded %d facts, want the approval", n)
	}
	moved, err := l.Rules(ctx)
	must(t, err)
	if moved != 1 || stepState(t, pool, "R2:1-2") != "done" {
		t.Fatalf("moved %d; the step is %s", moved, stepState(t, pool, "R2:1-2"))
	}
}

// Review 152 B2: a source that does not answer is cut off after the read
// timeout; the pass goes on and records nothing for it.
func TestSlowSourceIsCutOff(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-1", `{"approval_executed":16}`, [2]string{"approval", "16"})
	l := &planloop.Loop{DB: pool, ReadTimeout: 100 * time.Millisecond, Sources: planloop.Sources{
		Approval: func(ctx context.Context, _ int64) (seenfacts.ApprovalFact, bool) {
			<-ctx.Done()
			return seenfacts.ApprovalFact{}, false
		},
	}}
	start := time.Now()
	n, err := l.Facts(ctx)
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
	l := &planloop.Loop{DB: pool, ReadTimeout: 100 * time.Millisecond, Sources: planloop.Sources{
		Deployed: func(ctx context.Context, _ string) (string, map[string]any, bool) {
			<-ctx.Done()
			return "", nil, false
		},
	}}
	start := time.Now()
	moved, err := l.Rules(ctx)
	must(t, err)
	if moved != 0 || time.Since(start) > 5*time.Second {
		t.Fatalf("moved %d after %v", moved, time.Since(start))
	}
}

// The plan decisions decide plan changes: once the Owner answers a card with
// the first option, the change lands.
//
// Origin: 2026-11-19, the controller split. The controller's
// TestStartDecidesPlanChanges ran this through its timers; the pass is
// planloop's now, so it is called here as the controller's step calls it.
func TestPlanDecisionsApplyAnAnsweredChange(t *testing.T) {
	pool := setup(t)
	out, err := planstore.ProposePlanChange(ctx, pool, actor.Writer{Actor: "thread:s1", Token: "operator"}, "k1", "R2", "a new item",
		[]planstore.PlanChange{{Op: "add_node", Parent: "R2", Kind: "item", Title: "second"}},
		func(context.Context, ownercard.Card) (string, error) { return "q_9", nil })
	must(t, err)
	if out["question_id"] != "q_9" {
		t.Fatalf("proposal: %v", out)
	}
	w := &world{questions: map[string]seenfacts.Question{"q_9": {ID: "q_9", Status: seenfacts.QuestionAnswered, Option: ownercard.CardApplyKey,
		AnsweredBy: "owner", AnsweredAt: "2026-10-14T20:00:00Z"}}}
	l := &planloop.Loop{DB: pool, Sources: w.sources()}
	_, err = l.PlanDecisions(ctx)
	must(t, err)
	var n int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.1'`).Scan(&n))
	if n != 1 {
		t.Fatal("the pass did not apply the answered change")
	}
}

// A done step with no standing condition cannot move again, so a pass leaves
// its row alone: a pass that locked it would wait here for the holder.
//
// Origin: 2026-11-19, PROD's agentrt-facts-rules locked 131 such rows each pass.
func TestRulesLeavesASettledStepUnlocked(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-1", `{"owner_signoff":"settled"}`)
	must(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := planstore.SetStepState(ctx, tx, "controller", "R2", "R2:1-1", "done", "test")
		return err
	}))
	held, err := pool.Begin(ctx)
	must(t, err)
	defer func() { _ = held.Rollback(ctx) }()
	_, err = held.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = 'R2:1-1' FOR UPDATE`)
	must(t, err)
	short, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	l := &planloop.Loop{DB: pool}
	moved, err := l.Rules(short)
	must(t, err)
	if moved != 0 {
		t.Fatalf("moved %d steps", moved)
	}
}

// A deployment read that needs the runtime's pool of its own still works when
// the pool has a single connection: the read happens before the record's
// transaction takes it, so the deployment is recorded at once instead of
// after the read timeout, with nothing recorded.
//
// Origin: 2026-11-19, STG's facts step took 10.004-10.009 s each pass: its
// pool of two had one connection on the bus listener and the record's
// transaction held the other while the deployment read asked for a third.
func TestDeployReadDoesNotWaitForTheRecordsConnection(t *testing.T) {
	pool := setup(t)
	setRule(t, pool, "R2:1-1", `{"gates_passed":[{"test":"T19","spec_sha256":"`+strings.Repeat("ab", 32)+`"}],"env":"stg"}`)
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	must(t, err)
	cfg.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, cfg)
	must(t, err)
	defer one.Close()
	l := &planloop.Loop{DB: one, ReadTimeout: 2 * time.Second, Sources: planloop.Sources{
		Deployed: func(ctx context.Context, _ string) (string, map[string]any, bool) {
			var n int
			if err := one.QueryRow(ctx, `SELECT 1`).Scan(&n); err != nil {
				return "", nil, false
			}
			return strings.Repeat("c", 40), map[string]any{"delivery_run": "run-1"}, true
		},
	}}
	start := time.Now()
	n, err := l.Facts(ctx)
	must(t, err)
	if n != 1 || time.Since(start) > time.Second {
		t.Fatalf("recorded %d after %v; want the deployment at once", n, time.Since(start))
	}
}

// TestMain binds the plan code's ports these passes reach, as the tests'
// own: no plan-standing rule and no card-auto-answer rule are in force and
// no plan has external work (the test database has none of them, so the
// real binding, planwiring's, answers the same), and a card body is counted
// in bytes under a limit no test card reaches. planwiring wires several
// domains, so the plan's own tests do not import it (2026-11-19).
func TestMain(m *testing.M) {
	planstore.BindPlanRules(planstore.RulePorts{
		PlanRuleInForce:      func(context.Context, pgx.Tx) (*planstore.PlanRule, error) { return nil, nil },
		CardAutoKindsInForce: func(context.Context, pgx.Tx) ([]string, error) { return nil, nil },
	})
	planstore.BindPlanExternalWork(planstore.ExternalWorkPorts{
		HasWork: func(context.Context, pgx.Tx, string) (bool, error) { return false, nil },
	})
	planstore.BindPlanQuestions(planstore.QuestionPorts{BodyBytes: func(body string) int { return len(body) }, BodyLimit: 1 << 20})
	os.Exit(m.Run())
}
