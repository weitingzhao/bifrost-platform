package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestExternalFactsIdempotent(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := store.ApprovalFact{Number: 16, Status: "pending", Action: "deploy", Tier: "T2", CreatedAt: at}
	q := store.Question{ID: "q_external", Status: "open", Number: 1, CreatedAt: at.Format(time.RFC3339)}
	qb, err := q.Body()
	ok(t, err)
	p := store.PipelineRun{Run: "ci-1", UID: "uid-1", Pipeline: "ci", Kind: "ci", Result: "Succeeded", CreatedAt: at, StartedAt: at, CompletedAt: at}
	record := func(kind string, body map[string]any, want int) {
		t.Helper()
		n, e := store.RecordExternal(ctx, w.rt, kind, []map[string]any{body})
		ok(t, e)
		if n != want {
			t.Fatalf("%s recorded %d, want %d", kind, n, want)
		}
	}
	for _, f := range []struct {
		k string
		b map[string]any
	}{{"approval_seen", a.Body()}, {"question_seen", qb}, {"pipeline_seen", p.Body()}} {
		record(f.k, f.b, 1)
		record(f.k, f.b, 0)
	}
	for i := 0; i < 3; i++ {
		changed, e := store.SeeApproval(ctx, w.rt, 16, func(context.Context) (store.ApprovalFact, bool) { return a, true })
		ok(t, e)
		if changed {
			t.Fatal("approval alternated")
		}
		changed, e = store.SeeQuestion(ctx, w.rt, q.ID, questionIs(q))
		ok(t, e)
		if changed {
			t.Fatal("question alternated")
		}
		record("approval_seen", a.Body(), 0)
		record("question_seen", qb, 0)
	}
	a.Status = store.ApprovalExecuted
	record("approval_seen", a.Body(), 1)
	record("approval_seen", a.Body(), 0)
	q.Status, q.Option, q.AnsweredBy, q.AnsweredAt = "answered", "a", "owner", at.Format(time.RFC3339)
	qb, err = q.Body()
	ok(t, err)
	record("question_seen", qb, 1)
	record("question_seen", qb, 0)
	w.rule(t, s1, `{"all":[{"approval_executed":16},{"question_answered":{"question":"q_external","option":"a"}}]}`, [2]string{"approval", "16"}, [2]string{"question", q.ID})
	if got := w.evaluate(t, s1, &deployments{}); got != "done" {
		t.Fatalf("external facts did not satisfy atoms: %s", got)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		as, e := store.SeenCopiesForTest(ctx, tx, "approval_seen", "approval", []string{"16"})
		if e != nil {
			return e
		}
		qs, e := store.SeenCopiesForTest(ctx, tx, "question_seen", "question", []string{q.ID})
		if e != nil {
			return e
		}
		if len(as) != 2 || as[1].Status != store.ApprovalExecuted || len(qs) != 2 || qs[1].Option != "a" || qs[1].AnsweredBy != "owner" || qs[1].AnsweredAt != q.AnsweredAt {
			t.Fatalf("timeline copies: %+v %+v", as, qs)
		}
		return nil
	}))

	// A historical copy with the old shape receives a single upgrade.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, e := store.Append(ctx, tx, store.Event{Stream: store.ExternalStream, Type: "question_seen", Actor: "controller", Body: map[string]any{"question": q.ID, "status": "open"}})
		return e
	}))
	record("question_seen", qb, 1)
	record("question_seen", qb, 0)
}

func TestLandResultsCollectionIdempotent(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	at := time.Now().UTC()
	p := store.PipelineRun{Run: "land-one", UID: "uid-one", Pipeline: "bifrost-land", Kind: "other", Result: "Succeeded", CreatedAt: at, StartedAt: at, CompletedAt: at}
	for i, want := range []int{1, 0, 1, 0} {
		if i == 2 {
			p.LandedCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			p.ChangeIDs = "Ione Itwo"
			p.Outcome = "landed"
		}
		n, err := store.RecordExternal(ctx, w.rt, "pipeline_seen", []map[string]any{p.Body()})
		if err != nil || n != want {
			t.Fatalf("pass %d: n=%d err=%v", i, n, err)
		}
	}
}

func TestExternalFactsVocab(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := store.PipelineRun{Run: "run", UID: "uid", Pipeline: "ci", Kind: "ci", Result: "Failed", CreatedAt: at, StartedAt: at, CompletedAt: at}
	for _, field := range []string{"unknown", "created_at", "reason"} {
		b := p.Body()
		b[field] = "bad value"
		n, err := store.RecordExternal(ctx, w.rt, "pipeline_seen", []map[string]any{b, p.Body()})
		if err == nil {
			t.Fatalf("accepted %s", field)
		}
		if field == "unknown" && n != 1 {
			t.Fatalf("bad fact stopped valid fact: %d", n)
		}
	}
}

func TestExternalFactsBackfill(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	facts := []map[string]any{}
	for _, id := range []string{"q_one", "q_two", "q_three"} {
		b, e := (store.Question{ID: id, Status: "open", Number: 1}).Body()
		ok(t, e)
		facts = append(facts, b)
	}
	n, e := store.RecordExternal(ctx, w.rt, "question_seen", facts)
	ok(t, e)
	if n != len(facts) {
		t.Fatalf("first pass: %d", n)
	}
	n, e = store.RecordExternal(ctx, w.rt, "question_seen", facts)
	ok(t, e)
	if n != 0 {
		t.Fatalf("second pass: %d", n)
	}
}

func TestAcceptedCIReuseRecordedOnce(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for i := 0; i < 3; i++ {
		recorded, err := store.RecordCIReuse(ctx, w.rt, "bifrost-platform", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "ci-original", "original-uid")
		ok(t, err)
		if recorded != (i == 0) {
			t.Fatalf("pass %d recorded %v", i, recorded)
		}
	}
	var count int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type='ci_reused'`).Scan(&count))
	if count != 1 {
		t.Fatalf("reuses: %d", count)
	}
}
