package planstore_test

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
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// The import's decision (planimportcard.go, the branch in plandecide.go):
// once, whatever runs it at once; refused when its request does not read
// back; its card acknowledged once (review 313 B1). What it leaves when it
// fails half way needs the process's test hook and stays in store
// (TestImportDecisionFailsHalfWay).

func proposeImport(t *testing.T, pool *pgxpool.Pool, req planimport.Request, a *asker) string {
	t.Helper()
	out, err := planstore.ProposePlanImport(ctx, pool, wr("thread:s1"), "k1", "round 2 into the runtime", req, a.ask)
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

// Origin: moved from store/planimportdecide_test.go (TestImportDecisionOnce), 2026-11-18 (TD-582).
func TestImportDecisionOnce(t *testing.T) {
	t.Parallel()
	pool, req := importDB(t)
	a := &asker{}
	q := proposeImport(t, pool, req, a)
	a.answer(q, seenfacts.QuestionAnswered, planstore.CardApplyKey)
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := planstore.DecidePlanChanges(ctx, pool, a.question, nil)
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

// Origin: moved from store/planimportdecide_test.go (TestImportDecisionBadRequest), 2026-11-18 (TD-582).
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
			ok(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
				_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream("R2"), Type: "plan_change_proposed", Actor: "controller",
					Body: map[string]any{"changes": []any{map[string]any{"op": "import", "artifact": id}}, "reason": "test", "standing": false,
						"question_id": "q_x"}})
				return err
			}))
			a := &asker{}
			a.answer("q_x", seenfacts.QuestionAnswered, planstore.CardApplyKey)
			n, err := planstore.DecidePlanChanges(ctx, pool, a.question, nil)
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

// Origin: moved from store/planimportdecide_test.go (TestImportDecisionAcknowledged), 2026-11-18 (TD-582).
func TestImportDecisionAcknowledged(t *testing.T) {
	t.Parallel()
	pool, req := importDB(t)
	a := &asker{}
	q := proposeImport(t, pool, req, a)
	a.answer(q, seenfacts.QuestionAnswered, planstore.CardApplyKey)
	failing := func(context.Context, string) error { return errors.New("question store down") }
	n, err := planstore.DecidePlanChanges(ctx, pool, a.question, failing)
	if n != 1 || err == nil {
		t.Fatalf("a pass whose acknowledgement fails: %d decided, %v", n, err)
	}
	for i := 0; i < 2; i++ {
		_, err = planstore.DecidePlanChanges(ctx, pool, a.question, a.ack)
		ok(t, err)
	}
	if a.acked[q] != 1 || len(a.acked) != 1 {
		t.Fatalf("acknowledged %v; want %s once", a.acked, q)
	}
}
