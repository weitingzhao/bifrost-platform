package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func pv[T any](v T) *T { return &v }

// started claims the oldest ready task with tool on host and starts it.
func (w *world) started(t *testing.T, host string, tool store.Tool) store.Fence {
	t.Helper()
	c := w.claim(t, host, store.LeaseTTL, tool)
	if c == nil {
		t.Fatalf("%s on %s claimed nothing", tool.Executor, host)
	}
	f := fence(c, host)
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	return f
}

func (w *world) cooldown(t *testing.T, pool string) *time.Time {
	t.Helper()
	var until *time.Time
	if err := w.rt.QueryRow(ctx, `SELECT cooldown_until FROM agentrt.vendor_pool WHERE vendor_pool_id = $1`, pool).Scan(&until); err != nil {
		t.Fatalf("pool %s: %v", pool, err)
	}
	return until
}

func (w *world) ledger(t *testing.T, attempt string) map[string]float64 {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT unit, amount::float8 FROM agentrt.budget_ledger WHERE attempt_id = $1`, attempt)
	ok(t, err)
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var unit string
		var n float64
		ok(t, rows.Scan(&unit, &n))
		out[unit] += n
	}
	ok(t, rows.Err())
	return out
}

func TestRefusalCoolsThePoolAndClaimsPassTheToolBy(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	f := w.started(t, "h1", cursor)
	out, err := store.ComputedWith(ctx, w.rt, f, store.Ending{ExitCode: pv(1), EndHow: "exit", Conclusion: "failed",
		Usage: &store.Usage{Pool: "cursor:owner:monthly", Tokens: pv(int64(120))},
		Pools: []store.PoolReading{{Pool: "cursor:owner:monthly", Limited: true, Text: "You've hit your usage limit"}}})
	ok(t, err)
	if out["usage_refused"] != nil {
		t.Fatalf("usage refused: %v", out["usage_refused"])
	}
	if got := w.ledger(t, f.AttemptID); got["tokens"] != 120 || got["wall_s"] < 0 || len(got) != 2 {
		t.Fatalf("ledger = %v; want wall_s and 120 tokens", got)
	}
	until := w.cooldown(t, "cursor:owner:monthly")
	if until == nil || until.Sub(time.Now()) < 5*time.Hour || until.Sub(time.Now()) > 7*time.Hour {
		t.Fatalf("cooldown until %v; want about 6 hours from now (no reset named)", until)
	}
	// The next task goes to no tool of the cooled account, and does go to
	// another tool.
	w.ready(t, store.Task{})
	if c := w.claim(t, "h2", store.LeaseTTL, cursor); c != nil {
		t.Fatalf("cursor of the cooled account claimed %s", c.TaskID)
	}
	if c := w.claim(t, "h3", store.LeaseTTL, cursor, codex); c == nil || c.Tool.Executor != "exec:codex" {
		t.Fatalf("claim = %+v; want codex, the cooled cursor passed by", c)
	}
}

func TestAVendorReadingOfASpentWindowCoolsUntilItsReset(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	f := w.started(t, "h1", codex)
	reset := time.Now().Add(30 * time.Hour).UTC().Truncate(time.Second)
	_, err := store.ComputedWith(ctx, w.rt, f, store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done",
		Usage: &store.Usage{Pool: "codex:owner:weekly", Tokens: pv(int64(3600))},
		Pools: []store.PoolReading{{Pool: "codex:owner:weekly", UsedFraction: pv(1.0), ResetsAt: &reset}}})
	ok(t, err)
	if until := w.cooldown(t, "codex:owner:weekly"); until == nil || !until.Equal(reset) {
		t.Fatalf("cooldown until %v; want the reset %v", until, reset)
	}
	var source string
	ok(t, w.rt.QueryRow(ctx, `SELECT measure_source FROM agentrt.vendor_pool WHERE vendor_pool_id = 'codex:owner:weekly'`).Scan(&source))
	if source != "reading" {
		t.Fatalf("source = %q; want reading", source)
	}
	// A reading with quota left cools nothing.
	w.ready(t, store.Task{})
	f2 := w.started(t, "h2", claude)
	_, err = store.ComputedWith(ctx, w.rt, f2, store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done",
		Usage: &store.Usage{Pool: "claude:owner:weekly", USD: pv(0.25)},
		Pools: []store.PoolReading{{Pool: "claude:owner:short", UsedFraction: pv(0.3), ResetsAt: &reset}}})
	ok(t, err)
	if until := w.cooldown(t, "claude:owner:short"); until != nil {
		t.Fatalf("a pool with quota left cools until %v", until)
	}
	if got := w.ledger(t, f2.AttemptID); got["usd"] != 0.25 {
		t.Fatalf("ledger = %v; want 0.25 usd", got)
	}
}

// A report the platform cannot take never fails the ending (as a heartbeat's
// actions, review 264 A1): another tool's pool, a fraction over 1.
func TestABadReportIsDroppedAndTheAttemptIsComputed(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for i, e := range []store.Ending{
		{Usage: &store.Usage{Pool: "codex:owner:weekly", Tokens: pv(int64(5))}},
		{Pools: []store.PoolReading{{Pool: "cursor:owner:monthly", UsedFraction: pv(2.0)}}},
		{Pools: []store.PoolReading{{Pool: "cursor:owner:monthly", Text: "bad\x00text"}}},
		// another account's pool of the same tool (review 274 A4)
		{Pools: []store.PoolReading{{Pool: "cursor:someone-else:monthly", Limited: true}}},
		{Usage: &store.Usage{Pool: "cursor:someone-else:monthly", Tokens: pv(int64(5))}},
	} {
		w.ready(t, store.Task{})
		f := w.started(t, []string{"h1", "h2", "h3", "h4", "h5"}[i], cursor)
		e.ExitCode, e.EndHow, e.Conclusion = pv(0), "exit", "done"
		out, err := store.ComputedWith(ctx, w.rt, f, e)
		ok(t, err)
		if out["computed"] != true || out["usage_refused"] == nil {
			t.Fatalf("case %d: out = %v; want computed with the report refused", i, out)
		}
		if got := w.ledger(t, f.AttemptID); len(got) != 1 || got["wall_s"] < 0 {
			t.Fatalf("case %d: ledger = %v; want the wall time alone", i, got)
		}
	}
	var pools int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.vendor_pool`).Scan(&pools))
	if pools != 0 {
		t.Fatalf("%d pools made from refused reports", pools)
	}
}

func TestComputedWithUsageIsIdempotent(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	f := w.started(t, "h1", codex)
	e := store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done", Usage: &store.Usage{Pool: "codex:owner:weekly", Tokens: pv(int64(42))}}
	_, err := store.ComputedWith(ctx, w.rt, f, e)
	ok(t, err)
	_, err = store.ComputedWith(ctx, w.rt, f, e)
	ok(t, err)
	if got := w.ledger(t, f.AttemptID); got["tokens"] != 42 {
		t.Fatalf("ledger = %v; want 42 tokens once", got)
	}
	// A retry that measured differently (agentd restarted and read its output
	// again) gets the first answer: the ending is never held by its usage.
	e.Usage.Tokens = pv(int64(43))
	_, err = store.ComputedWith(ctx, w.rt, f, e)
	ok(t, err)
	if got := w.ledger(t, f.AttemptID); got["tokens"] != 42 {
		t.Fatalf("ledger = %v; want the first report's 42 tokens", got)
	}
	// Another ending under the same key is still a conflict.
	e.Conclusion = "failed"
	_, err = store.ComputedWith(ctx, w.rt, f, e)
	is(t, err, store.ErrConflict)
}

// Review 274 B6: a refusal handed over after the reset it named cools
// nothing; one that named a reset past belief counts as naming none.
func TestARefusalAfterItsResetCoolsNothing(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	w.ready(t, store.Task{})
	fs := []store.Fence{w.started(t, "h1", claude), w.started(t, "h2", codex)}
	past, far := time.Now().Add(-time.Minute), time.Now().Add(400*24*time.Hour)
	_, err := store.ComputedWith(ctx, w.rt, fs[0], store.Ending{ExitCode: pv(1), EndHow: "exit", Conclusion: "failed",
		Pools: []store.PoolReading{{Pool: "claude:owner:short", Limited: true, ResetsAt: &past}}})
	ok(t, err)
	if until := w.cooldown(t, "claude:owner:short"); until != nil {
		t.Fatalf("a refusal after its reset cools until %v", until)
	}
	_, err = store.ComputedWith(ctx, w.rt, fs[1], store.Ending{ExitCode: pv(1), EndHow: "exit", Conclusion: "failed",
		Pools: []store.PoolReading{{Pool: "codex:owner:weekly", Limited: true, ResetsAt: &far}}})
	ok(t, err)
	if until := w.cooldown(t, "codex:owner:weekly"); until == nil || until.Sub(time.Now()) > 7*time.Hour {
		t.Fatalf("a refusal with an unbelievable reset cools until %v; want about 6 hours", until)
	}
}
