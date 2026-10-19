package store_test

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The plan import under concurrency and failure (review 310 B1, B2); its
// results through the routes are in agentrtapi/planimport_test.go.

func importDB(t *testing.T) (*pgxpool.Pool, planimport.Request) {
	t.Helper()
	db := agentrttest.Migrated(t)
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	ok(t, err)
	text, err := os.ReadFile("../planimport/testdata/ledger.md")
	ok(t, err)
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	return db.Pool(t, db.Runtime), req
}

// rows counts what an import writes: nodes, events, artifacts and their
// bodies, streams, outbox rows.
func rows(t *testing.T, pool *pgxpool.Pool) [6]int {
	t.Helper()
	var c [6]int
	ok(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.plan_node), (SELECT count(*) FROM agentrt.event),
	  (SELECT count(*) FROM agentrt.artifact), (SELECT count(*) FROM agentrt.artifact_blob),
	  (SELECT count(*) FROM agentrt.event_stream), (SELECT count(*) FROM agentrt.outbox)`).Scan(&c[0], &c[1], &c[2], &c[3], &c[4], &c[5]))
	return c
}

// waitingOnTreeLock reports whether some session waits for an advisory lock.
func waitingOnTreeLock(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var n int
	ok(t, pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
	  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n))
	return n > 0
}

type importOutcome struct {
	res store.ImportResult
	err error
}

// Two imports of one root at once: the second waits for the first's lock,
// then finds its import (the same request) or refuses (a different one).
func TestPlanImportConcurrent(t *testing.T) {
	for _, differ := range []bool{false, true} {
		name := "same request"
		if differ {
			name = "different request"
		}
		t.Run(name, func(t *testing.T) {
			pool, req := importDB(t)
			other := req
			if differ {
				other.Nodes = append([]planimport.Node(nil), req.Nodes...)
				other.Nodes[0].Title = req.Nodes[0].Title + "（改）"
				ev := *req.Nodes[0].Evidence
				ev.Line = strings.Replace(ev.Line, req.Nodes[0].Title, other.Nodes[0].Title, 1)
				other.Nodes[0].Evidence = &ev
			}
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unhook := store.SetHook(func(point, arg string) {
				if point == "plan import locked" {
					once.Do(func() {
						close(held)
						<-release
					})
				}
			})
			defer unhook()
			a, b := make(chan importOutcome, 1), make(chan importOutcome, 1)
			go func() {
				res, err := store.ImportPlan(ctx, pool, "controller", req)
				a <- importOutcome{res, err}
			}()
			<-held
			go func() {
				res, err := store.ImportPlan(ctx, pool, "controller", other)
				b <- importOutcome{res, err}
			}()
			deadline := time.Now().Add(10 * time.Second)
			for !waitingOnTreeLock(t, pool) {
				if time.Now().After(deadline) {
					t.Fatal("the second import never waited for the tree lock")
				}
				time.Sleep(20 * time.Millisecond)
			}
			close(release)
			ra, rb := <-a, <-b
			ok(t, ra.err)
			if !ra.res.Imported {
				t.Fatalf("first import = %+v", ra.res)
			}
			after := rows(t, pool)
			if differ {
				if !errors.Is(rb.err, store.ErrInvalid) || !strings.Contains(rb.err.Error(), "not imported from this request") {
					t.Fatalf("a different request at the same time: %v", rb.err)
				}
			} else {
				ok(t, rb.err)
				if rb.res.Imported || rb.res.EventID != ra.res.EventID {
					t.Fatalf("second import = %+v, first %+v", rb.res, ra.res)
				}
			}
			if _, err := store.VerifyStream(ctx, pool, store.PlanStream("R2")); err != nil {
				t.Fatal(err)
			}
			if again, err := store.ImportPlan(ctx, pool, "controller", req); err != nil || again.Imported || rows(t, pool) != after {
				t.Fatalf("a third import: %+v %v", again, err)
			}
		})
	}
}

// An import that fails after it has written done steps' notes and moved them
// to done leaves nothing: no node, event, artifact, stream or outbox row.
func TestPlanImportRollbackAfterManual(t *testing.T) {
	pool, req := importDB(t)
	before := rows(t, pool)
	// The fixture's fifth step (2.1-2) comes after three done steps of 2.0
	// and the done 2.1-1, each with its note and its state change.
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
		_, _ = store.ImportPlan(ctx, pool, "controller", req)
	}()
	unhook()
	if got := rows(t, pool); got != before {
		t.Fatalf("a failed import left rows: %v, were %v", got, before)
	}
	res, err := store.ImportPlan(ctx, pool, "controller", req)
	ok(t, err)
	if !res.Imported {
		t.Fatalf("the import after the failed one = %+v", res)
	}
}

// A request that says something its lines do not is refused before anything
// is written (review 310 A3).
func TestPlanImportRefusesInconsistentRequest(t *testing.T) {
	pool, req := importDB(t)
	before := rows(t, pool)
	for i := range req.Nodes {
		if req.Nodes[i].State == planimport.StatePlanned {
			req.Nodes[i].State = planimport.StateDone
			break
		}
	}
	if _, err := store.ImportPlan(ctx, pool, "controller", req); !errors.Is(err, store.ErrInvalid) ||
		!strings.Contains(err.Error(), "does not say what its line says") {
		t.Fatalf("an inconsistent request: %v", err)
	}
	if rows(t, pool) != before {
		t.Fatal("a refused request wrote something")
	}
}
