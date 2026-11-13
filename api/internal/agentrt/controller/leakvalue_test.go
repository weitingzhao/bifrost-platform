package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func leakValueMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// leakValueMarked replaces the projection the pass reads with one marked
// place and a marked-looking unattributed row, for the test's life.
func leakValueMarked(t *testing.T) {
	t.Helper()
	marked := store.LeakValueRow{Place: "A:1", Label: "api Agent runtime API", Mark: &store.LeakValueMark{Kind: "add_back", Since: "2026-W42"},
		Leaks: []store.LeakValueLeak{
			{Env: "prod", Week: "2026-W42", Record: "issue BI-1 classified fault (check 11), work M-7 task t_1"},
			{Env: "stg", Week: "2026-W43", Record: "STG release stg:bifrost-platform:1 try 1 verify failed (landing:bifrost-platform seq 3), member t_1"},
		}}
	stray := store.LeakValueRow{Place: store.LeakValueUnattributed, Label: store.LeakValueUnattributed, Mark: &store.LeakValueMark{Kind: "add_back", Since: "2026-W42"}}
	quiet := store.LeakValueRow{Place: "B:1", Label: "ui"}
	old := leakValueRead
	leakValueRead = func(context.Context, store.DB, time.Time, int) (store.LeakValueDoc, error) {
		return store.LeakValueDoc{Weeks: store.LeakValueWeeksDefault, Rows: []store.LeakValueRow{marked, quiet, stray}}, nil
	}
	t.Cleanup(func() { leakValueRead = old })
}

func leakValueGoals(t *testing.T, pool *pgxpool.Pool) (ids, texts []string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT mission_id, goal FROM agentrt.mission WHERE title LIKE 'Add a check back for %' ORDER BY mission_id`)
	leakValueMust(t, err)
	defer rows.Close()
	for rows.Next() {
		var id, g string
		leakValueMust(t, rows.Scan(&id, &g))
		ids, texts = append(ids, id), append(texts, g)
	}
	leakValueMust(t, rows.Err())
	return ids, texts
}

// No reader of main, no pass: a goal could not be proposed.
func TestLeakValuePassWithoutMainProposesNothing(t *testing.T) {
	leakValueMarked(t)
	c := &Controller{}
	if n, err := c.LeakValuePass(context.Background()); n != 0 || err != nil {
		t.Fatal(n, err)
	}
}

// One goal per marked place and week; none while the goal of an earlier
// week is open; another once it is not.
func TestLeakValuePassDedupe(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	leakValueMust(t, err)
	pool := db.Pool(t, db.Runtime)
	_, err = pool.Exec(ctx, agentrttest.DispatchRules)
	leakValueMust(t, err)
	leakValueMarked(t)
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	c := &Controller{DB: pool, Now: func() time.Time { return now },
		FullCI: &FullCI{Main: func(context.Context) (string, error) { return strings.Repeat("1", 40), nil }}}
	pass := func() int {
		t.Helper()
		n, err := c.LeakValuePass(ctx)
		leakValueMust(t, err)
		return n
	}
	if n := pass(); n != 1 {
		t.Fatal("the first pass proposed", n)
	}
	if n := pass(); n != 0 {
		t.Fatal("the same week proposed again", n)
	}
	ids, texts := leakValueGoals(t, pool)
	if len(ids) != 1 {
		t.Fatal("goals", ids)
	}
	for _, want := range []string{"api Agent runtime API (A:1)", "since 2026-W42", "issue BI-1 classified fault", "stg:bifrost-platform:1 try 1 verify failed",
		"add it back"} {
		if !strings.Contains(texts[0], want) {
			t.Fatalf("the goal lacks %q:\n%s", want, texts[0])
		}
	}
	var place, week, mission, needs string
	leakValueMust(t, pool.QueryRow(ctx, `SELECT body_json->'leak_value'->>'place', body_json->'leak_value'->>'week', body_json->'leak_value'->>'mission'
	  FROM agentrt.event WHERE event_stream_id = 'controller' AND event_type = 'note' AND body_json ? 'leak_value'`).Scan(&place, &week, &mission))
	if place != "A:1" || week != "2026-W45" || mission != ids[0] {
		t.Fatal("the note", place, week, mission)
	}
	leakValueMust(t, pool.QueryRow(ctx, `SELECT COALESCE(scope_json->>'needs_owner', '') FROM agentrt.mission WHERE mission_id = $1`, ids[0]).Scan(&needs))
	if needs != LeakValueNeedsOwner {
		t.Fatalf("needs owner %q", needs)
	}
	// The next week, the goal is still open: none.
	now = now.AddDate(0, 0, 7)
	if n := pass(); n != 0 {
		t.Fatal("proposed while the goal is open", n)
	}
	// Once it is not, the place gets its next goal.
	leakValueMust(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", ids[0], "stopped", "test") }))
	if n := pass(); n != 1 {
		t.Fatal("after the goal closed", n)
	}
	if n := pass(); n != 0 {
		t.Fatal("twice in a week", n)
	}
	if ids, _ = leakValueGoals(t, pool); len(ids) != 2 {
		t.Fatal("goals", ids)
	}
}
