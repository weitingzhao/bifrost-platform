package controller_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// concurrency inserts version v of the concurrency rule with body, as if the
// Owner had signed it (the fixture's way, agentrttest.DispatchRules).
func concurrency(t *testing.T, pool *pgxpool.Pool, v int, body string) {
	t.Helper()
	text := fmt.Sprintf("rule_id: concurrency\nrule_version: %d\ndirection: loosen\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\n%s",
		v, body)
	r, err := rules.Parse([]byte(text))
	must(t, err)
	doc, err := r.BodyJSON()
	must(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	    effective_at, expires_at) VALUES ('concurrency', $1, $2::jsonb, $3, 'loosen', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z')`,
		v, string(doc), text)
	must(t, err)
}

func effectiveSlots(t *testing.T, pool *pgxpool.Pool) (int, int) {
	t.Helper()
	var n, moves int
	must(t, pool.QueryRow(ctx, `SELECT COALESCE((capacity_json->>'effective_slots')::int, -1),
	  (SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'host:h1' AND event_type = 'host_slots_set')
	  FROM agentrt.host WHERE host_id = 'h1'`).Scan(&n, &moves))
	return n, moves
}

// R5 phase 2 step 3b (elastic slots, the Owner 2026-11-06 Q3): the slots
// pass gives a host its first count at once, moves it by the rule's policy
// from the headroom its claims report, at most once an interval, and at once
// when the policy is fixed or the count is outside the rule's range; under a
// rule without a slots block nothing is written.
func TestSlotsPassMovesByThePolicy(t *testing.T) {
	pool := setup(t)
	must(t, store.RegisterHost(ctx, pool, "owner", "h1", "dev"))
	var logs []string
	c := &controller.Controller{DB: pool, Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }}
	pass := func(want int) {
		t.Helper()
		n, err := c.Slots(ctx)
		if err != nil || n != want {
			t.Fatalf("pass: %d moves, %v; want %d", n, err, want)
		}
	}
	pass(0) // the fixture's rule has no slots block
	if n, moves := effectiveSlots(t, pool); n != -1 || moves != 0 {
		t.Fatalf("without a block: %d, %d moves", n, moves)
	}

	concurrency(t, pool, 2, "per_host: 3\nslots: {policy: aimd, min: 1, start: 2, interval_s: 600}\n")
	// The host's claim reports it overloaded: 16 on 8 CPUs.
	if cl, err := store.ClaimNext(ctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{{Executor: "exec:codex", Provider: "openai"}},
		Signals: &store.HostSignals{Load1: 16, CPUs: 8, MemAvail: 8 << 30, MemTotal: 16 << 30}}); err != nil || cl != nil {
		t.Fatalf("claim: %+v %v", cl, err)
	}
	pass(1)
	if n, moves := effectiveSlots(t, pool); n != 1 || moves != 1 || !strings.Contains(logs[len(logs)-1], "h1 2 -> 1 (aimd): load per CPU 200% is above 100%") {
		t.Fatalf("overloaded: %d, %d moves, log %q", n, moves, logs)
	}
	pass(0) // within the interval
	if n, err := store.GrantedSlots(ctx, pool, "h1"); err != nil || n != 1 {
		t.Fatalf("granted %d %v", n, err)
	}

	concurrency(t, pool, 3, "per_host: 3\nslots: {policy: fixed, min: 1, start: 3}\n")
	pass(1) // fixed follows the rule at once
	if n, _ := effectiveSlots(t, pool); n != 3 {
		t.Fatalf("fixed at 3: %d", n)
	}
	pass(0)

	concurrency(t, pool, 4, "per_host: 2\nslots: {policy: aimd, min: 1, start: 1}\n")
	pass(1) // 3 is outside 1..2: decided at once, and halved
	var reason string
	must(t, pool.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE event_stream_id = 'host:h1' AND event_type = 'host_slots_set'
	  ORDER BY seq DESC LIMIT 1`).Scan(&reason))
	if n, moves := effectiveSlots(t, pool); n != 1 || moves != 3 || !strings.HasPrefix(reason, "the rule's range is now 1..2; load per CPU 200%") {
		t.Fatalf("range moved: %d, %d moves, %q", n, moves, reason)
	}

	concurrency(t, pool, 5, "per_host: 3\n")
	pass(0)
	if n, err := store.GrantedSlots(ctx, pool, "h1"); err != nil || n != 3 {
		t.Fatalf("without a block again: granted %d %v, want per_host", n, err)
	}
}
