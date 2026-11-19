package controller_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/loopalert"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// TD-411 (STG 2026-10-20: the facts pass failed every 15 seconds on a check
// constraint for two hours, a warning each time). A loop that fails the same
// way FailuresToAlert times in a row (errors that differ only in ids and
// numbers) records one alert, listed by Progress, and once it passes again a
// note, so Progress lists it no more. A loop whose error keeps changing, or
// that passes between failures, is not alerted.
func TestALoopFailingTheSameWayIsAlertedOnce(t *testing.T) {
	pool := setup(t)
	c := &controller.Controller{DB: pool, Logf: func(string, ...any) {}}
	// run passes results in order, one a pass, as one loop; before (each) is
	// called ahead of the pass of that index; the pass after the last ends it.
	run := func(name string, results []error, before map[int]func()) {
		t.Helper()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		i := 0
		c.RunLoopForTest(ctx, name, time.Millisecond, func() error {
			if i >= len(results) {
				cancel()
				return nil
			}
			if f := before[i]; f != nil {
				f()
			}
			i++
			return results[i-1]
		})
	}
	same := func(n int) []error {
		var out []error
		for i := range n {
			out = append(out, fmt.Errorf(`task t_01M5DWQTA77Z5XYKMYBDA3590%d: ERROR: violates check constraint "task_check2" (SQLSTATE 23514)`, i))
		}
		return out
	}
	events := func(kind string) []string {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT COALESCE(body_json->>'text', body_json->>'loop') FROM agentrt.event
		  WHERE event_stream_id = $1 AND event_type = $2 ORDER BY seq`, loopalert.ControllerStream, kind)
		must(t, err)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			must(t, rows.Scan(&s))
			out = append(out, s)
		}
		return out
	}
	// What Progress lists as controller_alerts (planstore reads it through
	// its port, bound to loopalert.FailingLoops).
	failing := func() ([]string, error) {
		var alerts []map[string]any
		err := eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
			var err error
			alerts, err = loopalert.FailingLoops(ctx, tx)
			return err
		})
		if err != nil {
			return nil, err
		}
		var loops []string
		for _, a := range alerts {
			loops = append(loops, a["loop"].(string))
		}
		return loops, nil
	}

	// Four the same way, then three more: one alert, listed while it fails;
	// then it passes: a note, and Progress lists it no more.
	var whileFailing []string
	var readErr error
	run("agentrt-facts-rules", append(same(7), nil), map[int]func(){7: func() { whileFailing, readErr = failing() }})
	if a := events("controller_loop_failing"); len(a) != 1 || a[0] != "agentrt-facts-rules" {
		t.Fatalf("alerts %q; want one of agentrt-facts-rules", a)
	}
	if readErr != nil || len(whileFailing) != 1 || whileFailing[0] != "agentrt-facts-rules" {
		t.Fatalf("while it failed, Progress listed %q (%v)", whileFailing, readErr)
	}
	if n := events("controller_loop_recovered"); len(n) != 1 || !strings.HasPrefix(n[0], "agentrt-facts-rules passes again after 7 failures in a row") {
		t.Fatalf("notes %q", n)
	}
	if f, err := failing(); err != nil || len(f) != 0 {
		t.Fatalf("after it passed, Progress lists %q (%v)", f, err)
	}
	// Changing errors, and failures with a pass between: no alert.
	var changing []error
	for i := range 6 {
		changing = append(changing, fmt.Errorf("the %s read failed", []string{"approval", "question", "ci"}[i%3]))
	}
	run("agentrt-assess", changing, nil)
	run("agentrt-landings", append(append(same(3), nil), same(3)...), nil)
	if a := events("controller_loop_failing"); len(a) != 1 {
		t.Fatalf("alerts %q; want still the one", a)
	}
}

// flakyDB fails the next fail transactions it is asked to begin.
type flakyDB struct {
	eventlog.DB
	fail atomic.Int32
}

func (f *flakyDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	if f.fail.Load() > 0 {
		f.fail.Add(-1)
		return nil, errors.New("the database is away")
	}
	return f.DB.BeginTx(ctx, o)
}

// Review 466 A-1: an alert in the database is answered by a note once its
// loop passes, whatever happened between: the error changed before it
// passed; the note could not be written at the first pass; the workers
// restarted, so the loop that passes is a new one. One note an alert, however
// many passes follow; none for a loop never alerted (review 467 C-467-1).
func TestAnOpenAlertIsAnsweredOnceTheLoopPasses(t *testing.T) {
	pool := setup(t)
	db := &flakyDB{DB: pool}
	c := &controller.Controller{DB: db, Logf: func(string, ...any) {}}
	run := func(name string, results []error, before map[int]func()) {
		t.Helper()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		i := 0
		c.RunLoopForTest(ctx, name, time.Millisecond, func() error {
			if i >= len(results) {
				cancel()
				return nil
			}
			if f := before[i]; f != nil {
				f()
			}
			i++
			return results[i-1]
		})
	}
	same := func(n int, what string) []error {
		var out []error
		for i := range n {
			out = append(out, fmt.Errorf("%s %d", what, i))
		}
		return out
	}
	open := func(loop string) bool {
		t.Helper()
		o, err := loopalert.LoopAlertOpen(ctx, pool, loop)
		must(t, err)
		return o
	}
	notes := func(loop string) int {
		t.Helper()
		var n int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = $3
		  AND starts_with(body_json->>'text', $2)`, loopalert.ControllerStream, loop+" passes again", "controller_loop_recovered").Scan(&n))
		return n
	}
	// The error changes after the alert, then the loop passes.
	run("agentrt-card-rule", append(append(same(4, "the store is away"), errors.New("another error")), nil, nil, nil), nil)
	if open("agentrt-card-rule") {
		t.Fatal("changed error: the alert stays open after the loop passed")
	}
	// The note cannot be written at the first pass; it is at the next.
	run("agentrt-leases", append(same(4, "the lease read failed"), nil, nil, nil), map[int]func(){4: func() { db.fail.Store(1) }})
	if open("agentrt-leases") {
		t.Fatal("a note that failed once: the alert stays open")
	}
	// Alerted, then the workers restart: the new loop answers it at its first pass.
	run("agentrt-landings", same(4, "Gitea is away"), nil)
	if !open("agentrt-landings") {
		t.Fatal("the alert was not recorded")
	}
	run("agentrt-landings", []error{nil, nil}, nil)
	if open("agentrt-landings") {
		t.Fatal("after a restart: the alert stays open")
	}
	run("agentrt-landings", []error{nil}, nil)
	// A loop never alerted passes, also after a restart.
	run("agentrt-assess", []error{nil, nil}, nil)
	run("agentrt-assess", []error{nil}, nil)
	for loop, want := range map[string]int{"agentrt-card-rule": 1, "agentrt-leases": 1, "agentrt-landings": 1, "agentrt-assess": 0} {
		if got := notes(loop); got != want {
			t.Errorf("%s: %d notes that it passes again, want %d", loop, got, want)
		}
	}
	// What Progress lists as controller_alerts (loopalert.FailingLoops).
	var alerts []map[string]any
	must(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		alerts, err = loopalert.FailingLoops(ctx, tx)
		return err
	}))
	if len(alerts) != 0 {
		t.Fatalf("Progress lists %v", alerts)
	}
}
