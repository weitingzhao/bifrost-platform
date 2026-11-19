package planstore_test

import (
	"errors"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// The import's decision (planimportcard.go, the branch in plandecide.go):
// nothing left when it fails half way (review 313 B1). It needs the process's
// test hook, so it stays here; the rest of the decision's tests moved to
// planstore/planimportdecide_test.go (TD-582).

// Serial: it holds the package's test hook (SetHook).
func TestImportDecisionFailsHalfWay(t *testing.T) {
	pool, req := importDB(t)
	a := &asker{}
	q := proposeImport(t, pool, req, a)
	a.answer(q, seenfacts.QuestionAnswered, planstore.CardApplyKey)
	errFail := errors.New("fail here")
	unhook := setHook(func(point, arg string) {
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
		_, _ = planstore.DecidePlanChanges(ctx, pool, a.question, nil)
	}()
	unhook()
	if nodes, heads, decided := importCounts(t, pool); nodes != 0 || heads != 0 || decided != 0 {
		t.Fatalf("a decision that failed half way left %d nodes, %d heads, %d decisions", nodes, heads, decided)
	}
	n, err := planstore.DecidePlanChanges(ctx, pool, a.question, nil)
	ok(t, err)
	if nodes, heads, decided := importCounts(t, pool); n != 1 || nodes != 1+len(req.Nodes) || heads != 1 || decided != 1 {
		t.Fatalf("the next pass: %d decided, %d nodes, %d heads, %d decisions", n, nodes, heads, decided)
	}
}
