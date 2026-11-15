package agentrtapi_test

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The checker catches what it is there to catch. Reviews 449 and 451 found
// each of these passing an earlier checker (a regression case): a step moved
// out of dropped; a refused request that changed a row's other columns; a
// plan node no event of the request creates beside an import head on its
// tree. One retained case held before too: events that end elsewhere than
// the row. The cases about a task's moves and an attempt's left with their
// tables (0020).
func TestStateEventCheckerCatches(t *testing.T) {
	t.Parallel()
	// ev is a move; each sibling is "<id> <type>".
	ev := func(id int64, typ, from, to string, reason bool, siblings ...string) fuzzEvent {
		body := map[string]any{"from": from, "to": to}
		if reason {
			body["reason"] = "test"
		}
		e := fuzzEvent{id: id, typ: typ, body: body}
		for _, x := range siblings {
			n, kind, _ := strings.Cut(x, " ")
			sid, err := strconv.ParseInt(n, 10, 64)
			must(t, err)
			e.siblings = append(e.siblings, fuzzSibling{sid, kind})
		}
		return e
	}
	for name, c := range map[string]struct {
		table     string
		evs       []fuzzEvent
		from, now string
		want      string
	}{
		"regression: a step out of dropped":   {"plan_node", []fuzzEvent{ev(1, "plan_node_state_changed", "dropped", "active", true)}, "dropped", "active", "no transition table"},
		"retained: events that end elsewhere": {"mission", []fuzzEvent{ev(1, "mission_state_changed", "active", "paused", true)}, "active", "stopped", "end at"},
	} {
		if _, err := replayChain(c.table, c.evs, c.from, c.now); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want %q", name, err, c.want)
		}
	}
	f := newStateFuzz(t, 1, 1, map[string]*routeTally{}, map[string]int{})
	judge := func(code int, fn func(tx pgx.Tx) error) error {
		t.Helper()
		before, digest, since := f.snapshot(t), f.digest(t), f.lastEvent(t)
		must(t, store.InTx(ctx, f.pool, fn))
		return f.judge(t, "test", false, code, before, digest, since)
	}
	// A node written beside an import head on its tree, with no event of its own.
	must(t, store.InTx(ctx, f.pool, func(tx pgx.Tx) error {
		_, cerr := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "A", Title: "architecture", Root: "ARCH"})
		return cerr
	}))
	err := judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, ierr := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("ARCH"), Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: an import head"}}); ierr != nil {
			return ierr
		}
		_, ierr := tx.Exec(ctx, `INSERT INTO agentrt.plan_node (plan_node_id, parent_plan_node_id, node_kind, ordinal, label, title, created_by_actor_id)
		  VALUES ('ARCH:extra', 'ARCH', 'arch_row', 9, 'extra', 'not in the document', 'controller')`)
		return ierr
	})
	if err == nil || !strings.Contains(err.Error(), "no event of the request creates") {
		t.Errorf("regression: a node beside an import head: %v", err)
	}
	// A refused request that changed a column other than the state.
	var db string
	must(t, f.pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
	admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+db)
	must(t, err)
	t.Cleanup(func() { _ = admin.Close(ctx) })
	before, digest, since := f.snapshot(t), f.digest(t), f.lastEvent(t)
	for _, q := range []string{`SET session_replication_role = replica`, `UPDATE agentrt.mission SET title = 'edited' WHERE mission_id = 'M-1'`} {
		_, err = admin.Exec(ctx, q)
		must(t, err)
	}
	if err = f.judge(t, "test", false, http.StatusConflict, before, digest, since); err == nil || !strings.Contains(err.Error(), "agentrt.mission changed") {
		t.Errorf("regression: a refused request that edited a title: %v", err)
	}
	// A refused request while another transaction writes a host's poll, a
	// liveness column (ci-platform-tj5wx): nothing the request did.
	before, digest, since = f.snapshot(t), f.digest(t), f.lastEvent(t)
	_, err = f.pool.Exec(ctx, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() WHERE host_id = 'h1'`)
	must(t, err)
	if err = f.judge(t, "test", false, http.StatusUnauthorized, before, digest, since); err != nil {
		t.Errorf("regression: a refused request beside a host's poll: %v", err)
	}
	// Another column of the same row still counts.
	before, digest, since = f.snapshot(t), f.digest(t), f.lastEvent(t)
	_, err = admin.Exec(ctx, `UPDATE agentrt.host SET capacity_json = '{"slots": 9}' WHERE host_id = 'h1'`)
	must(t, err)
	if err = f.judge(t, "test", false, http.StatusUnauthorized, before, digest, since); err == nil || !strings.Contains(err.Error(), "agentrt.host changed") {
		t.Errorf("a refused request that changed a host's capacity: %v", err)
	}
}
