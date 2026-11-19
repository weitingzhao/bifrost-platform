package controller_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// setup is a migrated, seeded database with the dispatch rules. The mission
// and plan tree the plan passes read are agentrt/planloop's tests'
// (2026-11-19: those passes moved there).
func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := agentrttest.Migrated(t)
	_, err := actor.Seed(ctx, db.Connect(t, db.Owner))
	must(t, err)
	pool := db.Pool(t, db.Runtime)
	_, err = pool.Exec(ctx, agentrttest.DispatchRules)
	must(t, err)
	return pool
}

// world is the outside a step reads. Its next reads panic while panics is
// above zero.
type world struct {
	mu     sync.Mutex
	reads  int
	panics int // the next reads that panic
	done   bool
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

// Start runs the loops on timers; a pass that panics costs that pass only;
// cancelling the context stops them. Since 2026-11-19 the steps of the
// facts-and-rules pass and the loops of their own are the assembly's
// (Steps, Loops): here a step reads the world and settles once a read did
// not panic, and a loop of its own runs beside it.
func TestStart(t *testing.T) {
	pool := setup(t)
	w := &world{panics: 1}
	var mu sync.Mutex
	var logs []string
	looped := 0
	c := &controller.Controller{DB: pool, LeaseEvery: 20 * time.Millisecond, FactsEvery: 20 * time.Millisecond,
		Steps: []controller.Step{{Name: "facts", Run: func(context.Context) error {
			w.read()
			w.mu.Lock()
			w.done = true
			w.mu.Unlock()
			return nil
		}}},
		Loops: []controller.Loop{{Name: "agentrt-test-loop", Every: 20 * time.Millisecond, Run: func(context.Context) error {
			mu.Lock()
			looped++
			mu.Unlock()
			return nil
		}}},
		Logf: func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, fmt.Sprintf(format, args...))
		}}
	run, cancel := context.WithCancel(ctx)
	wait := c.Start(run)
	deadline := time.Now().Add(10 * time.Second)
	for {
		w.mu.Lock()
		done := w.done
		w.mu.Unlock()
		mu.Lock()
		ran := looped > 0
		mu.Unlock()
		if done && ran {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("the loops did not run the step and the loop")
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

// The steps of a pass run in their order, each once a pass, a failing one
// does not stop the next, and the pass's error joins theirs in that order.
//
// Origin: 2026-11-19, the controller split: the plan's facts, plan
// decisions, used answers and rules used to be four fixed calls in the
// facts-and-rules pass and are now steps the assembly binds; this holds the
// order and the joined error the four calls had.
func TestStepsRunInOrderAndJoinTheirErrors(t *testing.T) {
	pool := setup(t)
	var mu sync.Mutex
	var order []string
	var logs []string
	step := func(name string, err error) controller.Step {
		return controller.Step{Name: name, Run: func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			return err
		}}
	}
	errFacts, errRules := errors.New("facts failed"), errors.New("rules failed")
	c := &controller.Controller{DB: pool, LeaseEvery: time.Hour, FactsEvery: 20 * time.Millisecond,
		Steps: []controller.Step{step("facts", errFacts), step("plan decisions", nil), step("used answers", nil), step("rules", errRules)},
		Logf: func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, fmt.Sprintf(format, args...))
		}}
	run, cancel := context.WithCancel(ctx)
	wait := c.Start(run)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		n := len(order)
		mu.Unlock()
		if n >= 8 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			wait()
			t.Fatal("the pass did not run its steps twice")
		}
	}
	cancel()
	wait()
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(order[:8], ","); got != "facts,plan decisions,used answers,rules,facts,plan decisions,used answers,rules" {
		t.Fatalf("steps ran %s", got)
	}
	want := "agentrt-facts-rules: " + errors.Join(errFacts, errRules).Error()
	found := false
	for _, l := range logs {
		found = found || l == want
	}
	if !found {
		t.Fatalf("the pass did not log %q: %q", want, logs)
	}
}

// TestMain registers the artifact store's check of view artifacts: the plan
// code's (planstore registers it at start), which the controller does not
// link since its plan passes moved to agentrt/planloop (2026-11-19). The
// controller stores log tails only (a loop's alert), never a view artifact,
// so the check here passes every body.
func TestMain(m *testing.M) {
	artifact.RegisterCheck(func(string, []byte) error { return nil })
	os.Exit(m.Run())
}
