package store_test

import (
	"fmt"
	"math"
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

func TestARefusalNeverShortensACooldownButTheOwnerMay(t *testing.T) {
	w := newKernelWorld(t)
	long, short := time.Now().Add(10*time.Hour).UTC().Truncate(time.Second), time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	// Both attempts run before either is refused: once one is, claude takes nothing more.
	w.ready(t, store.Task{})
	w.ready(t, store.Task{})
	fs := []store.Fence{w.started(t, "h1", claude), w.started(t, "h2", claude)}
	for i, reset := range []time.Time{long, short} {
		f := fs[i]
		_, err := store.ComputedWith(ctx, w.rt, f, store.Ending{ExitCode: pv(1), EndHow: "exit", Conclusion: "failed",
			Pools: []store.PoolReading{{Pool: "claude:owner:short", Limited: true, ResetsAt: pv(reset)}}})
		ok(t, err)
	}
	if until := w.cooldown(t, "claude:owner:short"); until == nil || !until.Equal(long) {
		t.Fatalf("cooldown until %v; want the longer %v kept", until, long)
	}
	owner := store.Writer{Actor: "owner"}
	_, err := store.RecordOwnerReading(ctx, w.rt, owner, "r1", store.PoolReading{Pool: "claude:owner:short", UsedFraction: pv(1.0), ResetsAt: pv(short)})
	ok(t, err)
	if until := w.cooldown(t, "claude:owner:short"); until == nil || !until.Equal(short) {
		t.Fatalf("cooldown until %v; want the Owner's %v", until, short)
	}
	// Quota left: the Owner's reading lifts the cooldown, and claude claims again.
	_, err = store.RecordOwnerReading(ctx, w.rt, owner, "r2", store.PoolReading{Pool: "claude:owner:short", UsedFraction: pv(0.4), ResetsAt: pv(short)})
	ok(t, err)
	if until := w.cooldown(t, "claude:owner:short"); until == nil || until.After(time.Now()) {
		t.Fatalf("cooldown until %v; want lifted", until)
	}
	w.ready(t, store.Task{})
	if c := w.claim(t, "h3", store.LeaseTTL, claude); c == nil {
		t.Fatal("claude claimed nothing after the Owner lifted its cooldown")
	}
	// The same key again is the same answer; a reading the same key does not name is refused.
	_, err = store.RecordOwnerReading(ctx, w.rt, owner, "r2", store.PoolReading{Pool: "claude:owner:short", UsedFraction: pv(0.4), ResetsAt: pv(short)})
	ok(t, err)
	_, err = store.RecordOwnerReading(ctx, w.rt, owner, "r2", store.PoolReading{Pool: "claude:owner:short", UsedFraction: pv(0.5)})
	is(t, err, store.ErrConflict)
}

func TestTheOwnerReadingIsChecked(t *testing.T) {
	w := newKernelWorld(t)
	owner := store.Writer{Actor: "owner"}
	reset := time.Now().Add(time.Hour)
	for name, c := range map[string]struct {
		w   store.Writer
		key string
		r   store.PoolReading
	}{
		"no key":          {owner, "", store.PoolReading{Pool: "claude:owner:weekly", UsedFraction: pv(0.2)}},
		"no fraction":     {owner, "k", store.PoolReading{Pool: "claude:owner:weekly"}},
		"spent, no reset": {owner, "k", store.PoolReading{Pool: "claude:owner:weekly", UsedFraction: pv(1.0)}},
		"over 1":          {owner, "k", store.PoolReading{Pool: "claude:owner:weekly", UsedFraction: pv(1.5), ResetsAt: &reset}},
		"not a pool":      {owner, "k", store.PoolReading{Pool: "claude:weekly", UsedFraction: pv(0.2)}},
		"a refusal":       {owner, "k", store.PoolReading{Pool: "claude:owner:weekly", UsedFraction: pv(0.2), Limited: true}},
		"not the owner":   {store.Writer{Actor: "controller"}, "k", store.PoolReading{Pool: "claude:owner:weekly", UsedFraction: pv(0.2)}},
	} {
		if _, err := store.RecordOwnerReading(ctx, w.rt, c.w, c.key, c.r); err == nil {
			t.Errorf("%s: recorded", name)
		}
	}
}

// A report the platform cannot take never fails the ending (as a heartbeat's
// actions, review 264 A1): another tool's pool, a fraction over 1.
func TestABadReportIsDroppedAndTheAttemptIsComputed(t *testing.T) {
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

func TestListPoolsShowsPaceAndThisWindowsUsage(t *testing.T) {
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	f := w.started(t, "h1", claude)
	reset := time.Now().Add(48 * time.Hour)
	_, err := store.ComputedWith(ctx, w.rt, f, store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done",
		Usage: &store.Usage{Pool: "claude:owner:weekly", Tokens: pv(int64(900)), USD: pv(1.5)},
		Pools: []store.PoolReading{{Pool: "claude:owner:weekly", UsedFraction: pv(0.4), ResetsAt: &reset}}})
	ok(t, err)
	pools, err := store.ListPools(ctx, w.rt)
	ok(t, err)
	if len(pools) != 1 {
		t.Fatalf("pools = %+v", pools)
	}
	p := pools[0]
	if p.Tool != "claude" || p.Account != "owner" || p.Window != "weekly" || p.Source != "reading" || p.Cooling {
		t.Fatalf("pool = %+v", p)
	}
	// 0.4 used with 48 of 168 hours left: 0.4 − (1 − 48/168) ≈ −0.314.
	if p.Pace == nil || math.Abs(*p.Pace-(0.4-(1-48.0/168))) > 0.01 {
		t.Fatalf("pace = %v", p.Pace)
	}
	if p.Used["tokens"] != 900 || p.Used["usd"] != 1.5 {
		t.Fatalf("used = %v", p.Used)
	}
}

// Review 274 B6: a refusal handed over after the reset it named cools
// nothing; one that named a reset past belief counts as naming none.
func TestARefusalAfterItsResetCoolsNothing(t *testing.T) {
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

// Review 274 B7: the Owner's "spent" with a reset past belief is refused and
// the cooldown in force stays; only a reading under 1 lifts one.
func TestTheOwnersSpentReadingNeverLifts(t *testing.T) {
	w := newKernelWorld(t)
	owner := store.Writer{Actor: "owner"}
	tomorrow := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	_, err := store.RecordOwnerReading(ctx, w.rt, owner, "a", store.PoolReading{Pool: "claude:owner:weekly", UsedFraction: pv(1.0), ResetsAt: &tomorrow})
	ok(t, err)
	far := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = store.RecordOwnerReading(ctx, w.rt, owner, "b", store.PoolReading{Pool: "claude:owner:weekly", UsedFraction: pv(1.0), ResetsAt: &far})
	is(t, err, store.ErrInvalid)
	past := time.Now().Add(-time.Hour)
	_, err = store.RecordOwnerReading(ctx, w.rt, owner, "c", store.PoolReading{Pool: "claude:owner:weekly", UsedFraction: pv(0.5), ResetsAt: &past})
	is(t, err, store.ErrInvalid)
	if until := w.cooldown(t, "claude:owner:weekly"); until == nil || !until.Equal(tomorrow) {
		t.Fatalf("cooldown until %v; want %v kept", until, tomorrow)
	}
}

// Review 274 B5: a computed and the Owner's reading of the same new pool at
// once both commit; neither is chosen as a deadlock victim.
func TestComputedAndTheOwnersReadingAtOnce(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	owner := store.Writer{Actor: "owner"}
	reset := time.Now().Add(48 * time.Hour)
	for round := 0; round < 5; round++ {
		w.ready(t, store.Task{})
		f := w.started(t, fmt.Sprintf("h%d", round+1), cursor)
		p := "cursor:owner:monthly"
		errs := make(chan error, 2)
		go func() {
			_, err := store.ComputedWith(ctx, pool, f, store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done",
				Usage: &store.Usage{Pool: p, Tokens: pv(int64(1))}, Pools: []store.PoolReading{{Pool: p, UsedFraction: pv(0.3), ResetsAt: &reset}}})
			errs <- err
		}()
		go func() {
			_, err := store.RecordOwnerReading(ctx, pool, owner, fmt.Sprintf("r%d", round), store.PoolReading{Pool: p, UsedFraction: pv(0.4), ResetsAt: &reset})
			errs <- err
		}()
		for i := 0; i < 2; i++ {
			ok(t, <-errs)
		}
	}
}

// Review 285: a new reading with no reset is not shown against a reset that
// has passed since the reading before; a reset still ahead is kept.
func TestANewReadingDropsAResetAlreadyPast(t *testing.T) {
	w := newKernelWorld(t)
	owner := store.Writer{Actor: "owner"}
	soon := time.Now().Add(2 * time.Second).UTC()
	_, err := store.RecordOwnerReading(ctx, w.rt, owner, "a", store.PoolReading{Pool: "claude:owner:short", UsedFraction: pv(0.9), ResetsAt: &soon})
	ok(t, err)
	time.Sleep(2500 * time.Millisecond)
	_, err = store.RecordOwnerReading(ctx, w.rt, owner, "b", store.PoolReading{Pool: "claude:owner:short", UsedFraction: pv(0.2)})
	ok(t, err)
	var resets *time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT resets_at FROM agentrt.vendor_pool WHERE vendor_pool_id = 'claude:owner:short'`).Scan(&resets))
	if resets != nil {
		t.Fatalf("the new reading keeps the reset %v that has passed", resets)
	}
	ahead := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)
	_, err = store.RecordOwnerReading(ctx, w.rt, owner, "c", store.PoolReading{Pool: "claude:owner:short", UsedFraction: pv(0.3), ResetsAt: &ahead})
	ok(t, err)
	_, err = store.RecordOwnerReading(ctx, w.rt, owner, "d", store.PoolReading{Pool: "claude:owner:short", UsedFraction: pv(0.4)})
	ok(t, err)
	ok(t, w.rt.QueryRow(ctx, `SELECT resets_at FROM agentrt.vendor_pool WHERE vendor_pool_id = 'claude:owner:short'`).Scan(&resets))
	if resets == nil || !resets.Equal(ahead) {
		t.Fatalf("a reset still ahead was dropped: %v", resets)
	}
}
