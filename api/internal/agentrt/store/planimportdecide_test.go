package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The import's decision (planimportcard.go, the branch in plandecide.go):
// once, whatever runs it at once; refused when its request does not read
// back; nothing left when it fails half way; its card acknowledged once
// (review 313 B1).

func proposeImport(t *testing.T, pool *pgxpool.Pool, req planimport.Request, a *asker) string {
	t.Helper()
	out, err := store.ProposePlanImport(ctx, pool, wr("thread:s1"), "k1", "round 2 into the runtime", req, a.ask)
	ok(t, err)
	q, _ := out["question_id"].(string)
	if q == "" {
		t.Fatalf("import proposal = %v", out)
	}
	return q
}

func importCounts(t *testing.T, pool *pgxpool.Pool) (nodes, heads, decided int) {
	t.Helper()
	ok(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.plan_node),
	  (SELECT count(*) FROM agentrt.event WHERE event_type = 'legacy_imported'),
	  (SELECT count(*) FROM agentrt.event WHERE event_type = 'plan_change_decided')`).Scan(&nodes, &heads, &decided))
	return
}

func TestImportDecisionOnce(t *testing.T) {
	t.Parallel()
	pool, req := importDB(t)
	a := &asker{}
	q := proposeImport(t, pool, req, a)
	a.answer(q, store.QuestionAnswered, store.CardApplyKey)
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := store.DecidePlanChanges(ctx, pool, a.question, nil)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	nodes, heads, decided := importCounts(t, pool)
	if total != 1 || heads != 1 || decided != 1 || nodes != 1+len(req.Nodes) {
		t.Fatalf("two passes at once: %d decided, %d heads, %d decisions, %d nodes", total, heads, decided, nodes)
	}
}

// canonical is a request's canonical bytes and the id they are stored under.
func canonical(t *testing.T, req planimport.Request) ([]byte, string) {
	t.Helper()
	raw, err := json.Marshal(req)
	ok(t, err)
	doc, err := canon.Marshal(raw)
	ok(t, err)
	sum := sha256.Sum256(doc)
	return doc, "sha256:" + hex.EncodeToString(sum[:])
}

func TestImportDecisionBadRequest(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		store bool // store another request's body under the named id
		want  string
	}{
		"missing":            {false, "is not stored"},
		"another body there": {true, "does not read back as itself"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pool, req := importDB(t)
			_, id := canonical(t, req)
			if c.store {
				other := req
				other.Source.Commit = strings.Repeat("b", 40)
				body, _ := canonical(t, other)
				_, err := pool.Exec(ctx, `INSERT INTO agentrt.artifact (artifact_id, artifact_kind, media_type, size_bytes, created_by_actor_id)
				  VALUES ($1, 'other', 'application/json', $2, 'controller')`, id, len(body))
				ok(t, err)
				_, err = pool.Exec(ctx, `INSERT INTO agentrt.artifact_blob (artifact_id, body) VALUES ($1, $2)`, id, body)
				ok(t, err)
			}
			ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
				_, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("R2"), Type: "plan_change_proposed", Actor: "controller",
					Body: map[string]any{"changes": []any{map[string]any{"op": "import", "artifact": id}}, "reason": "test", "standing": false,
						"question_id": "q_x"}})
				return err
			}))
			a := &asker{}
			a.answer("q_x", store.QuestionAnswered, store.CardApplyKey)
			n, err := store.DecidePlanChanges(ctx, pool, a.question, nil)
			ok(t, err)
			var decision, why string
			ok(t, pool.QueryRow(ctx, `SELECT body_json->>'decision', COALESCE(body_json->>'error', '') FROM agentrt.event
			  WHERE event_type = 'plan_change_decided'`).Scan(&decision, &why))
			nodes, heads, _ := importCounts(t, pool)
			if n != 1 || decision != "failed" || !strings.Contains(why, c.want) || nodes != 0 || heads != 0 {
				t.Fatalf("%d decided: %s %q; %d nodes, %d heads", n, decision, why, nodes, heads)
			}
		})
	}
}

// Not parallel: it sets the package's hook.
func TestImportDecisionFailsHalfWay(t *testing.T) {
	pool, req := importDB(t)
	a := &asker{}
	q := proposeImport(t, pool, req, a)
	a.answer(q, store.QuestionAnswered, store.CardApplyKey)
	errFail := errors.New("fail here")
	unhook := store.SetHook(func(point, arg string) {
		if point == "plan import step" && arg == "R2:2.1-2" {
			panic(errFail)
		}
	})
	func() {
		defer func() {
			if r := recover(); r != errFail {
				t.Fatalf("recovered %v", r)
			}
		}()
		_, _ = store.DecidePlanChanges(ctx, pool, a.question, nil)
	}()
	unhook()
	if nodes, heads, decided := importCounts(t, pool); nodes != 0 || heads != 0 || decided != 0 {
		t.Fatalf("a decision that failed half way left %d nodes, %d heads, %d decisions", nodes, heads, decided)
	}
	n, err := store.DecidePlanChanges(ctx, pool, a.question, nil)
	ok(t, err)
	if nodes, heads, decided := importCounts(t, pool); n != 1 || nodes != 1+len(req.Nodes) || heads != 1 || decided != 1 {
		t.Fatalf("the next pass: %d decided, %d nodes, %d heads, %d decisions", n, nodes, heads, decided)
	}
}

func TestImportDecisionAcknowledged(t *testing.T) {
	t.Parallel()
	pool, req := importDB(t)
	a := &asker{}
	q := proposeImport(t, pool, req, a)
	a.answer(q, store.QuestionAnswered, store.CardApplyKey)
	failing := func(context.Context, string) error { return errors.New("question store down") }
	n, err := store.DecidePlanChanges(ctx, pool, a.question, failing)
	if n != 1 || err == nil {
		t.Fatalf("a pass whose acknowledgement fails: %d decided, %v", n, err)
	}
	for i := 0; i < 2; i++ {
		_, err = store.DecidePlanChanges(ctx, pool, a.question, a.ack)
		ok(t, err)
	}
	if a.acked[q] != 1 || len(a.acked) != 1 {
		t.Fatalf("acknowledged %v; want %s once", a.acked, q)
	}
}
