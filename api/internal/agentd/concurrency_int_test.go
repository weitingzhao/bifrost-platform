package agentd_test

import (
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// concurrencyTwo is the rule the Owner signs after slice ③: two live attempts
// a host.
const concurrencyTwo = `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
    effective_at, expires_at) VALUES ('concurrency', 2,
    '{"direction":"loosen","effective_at":"2026-01-02T00:00:00Z","expires_at":"2100-01-01T00:00:00Z","per_host":2,"rule_id":"concurrency","rule_version":2}',
    E'rule_id: concurrency\nrule_version: 2\ndirection: loosen\neffective_at: 2026-01-02T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\nper_host: 2\n',
    'loosen', 'test', 'test', '2026-01-02T00:00:00Z', '2100-01-01T00:00:00Z')`

// readyTask proposes another ready task of M-1 with the same spec.
func readyTask(t *testing.T, tx func(func(pgx.Tx) error)) string {
	t.Helper()
	id := store.NewTaskID()
	tx(func(tx pgx.Tx) error {
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# task\n"))
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
			Class: "go", Title: "t", SpecArtifact: spec, Effect: "none"}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", id)
	})
	return id
}

// W-31 2.4 slice ③: with per_host 2 one host leases two attempts and no
// third; they run side by side; Recover between claims leaves both alone; a
// stop ends one and the other is handed over.
func TestTwoAttemptsAtOnceOnOneHost(t *testing.T) {
	pool, _, base := platform(t)
	tx := func(fn func(pgx.Tx) error) { t.Helper(); must(t, store.InTx(ctx, pool, fn)) }
	tx(func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: per_host 2"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, concurrencyTwo)
		return err
	})
	readyTask(t, tx)
	readyTask(t, tx) // a third, that no third slot takes
	d := daemon(t, base, script(`sleep 2; printf '# done\n' > "$OUT/report.md"`), "", nil)
	var cls []*agentd.Claimed
	for i := 0; i < 2; i++ {
		cl, err := d.Client.Claim(ctx, agentd.MaxSlots-i, []agentd.Tool{cursor})
		if err != nil || cl == nil {
			t.Fatalf("claim %d = %+v %v", i, cl, err)
		}
		cls = append(cls, cl)
	}
	if cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor}); err != nil || cl != nil {
		t.Fatalf("a third live attempt on the host: %+v %v", cl, err)
	}
	stopped := cls[0].TaskID
	type result struct {
		out agentd.Outcome
		err error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i, cl := range cls {
		d.Hold(cl.AttemptID)
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := d.Run(ctx, cl)
			results[i] = result{out, err}
		}()
	}
	// Both run: two attempts running on h1 at once.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.attempt WHERE host_id = 'h1' AND state = 'running'`).Scan(&n))
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d attempts running at once", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
	must(t, d.Recover(ctx))
	tx(func(tx pgx.Tx) error {
		return store.SetControl(ctx, tx, "owner", "c-stop-one", "task", stopped, "stop", "test")
	})
	wg.Wait()
	for i, r := range results {
		switch {
		case cls[i].TaskID == stopped && (r.out.EndHow != "cancelled" || r.out.Submitted):
			t.Fatalf("the stopped one: %+v %v", r.out, r.err)
		case cls[i].TaskID != stopped && (r.err != nil || r.out.EndHow != "exit" || !r.out.Submitted):
			t.Fatalf("the other one: %+v %v", r.out, r.err)
		}
	}
	if d.Running() != 0 {
		t.Fatalf("%d still held after both ended", d.Running())
	}
}
