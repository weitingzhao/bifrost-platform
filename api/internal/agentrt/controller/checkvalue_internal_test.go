package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func checkValueSetup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(context.Background(), db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	if _, err := pool.Exec(context.Background(), agentrttest.DispatchRules); err != nil {
		t.Fatal(err)
	}
	return pool
}

// checkValueDoc is a table whose last full week is week (current after
// it), with the review row marked when marked.
func checkValueDoc(week, current string, marked bool) store.CheckValueDoc {
	n, zero := 3, 0
	row := store.CheckValueRow{Key: "review", Kind: "check", Label: "Review", Order: 3}
	for _, w := range []string{"2026-W41", "2026-W42", "2026-W43", week, current} {
		row.Cells = append(row.Cells, store.CheckValueCell{Week: w, Ran: &n, Caught: &zero, Unique: &zero, Noise: &zero})
	}
	if marked {
		row.Mark = &store.CheckValueMark{Kind: "delete", Since: "2026-W41"}
	}
	other := store.CheckValueRow{Key: "deterministic", Kind: "check", Label: "Deterministic verification", Order: 2}
	return store.CheckValueDoc{Weeks: []string{"2026-W41", "2026-W42", "2026-W43", week, current}, Rows: []store.CheckValueRow{other, row}}
}

func checkValueGoals(t *testing.T, pool *pgxpool.Pool) (missions []string, goals []string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT mission_id, goal FROM agentrt.mission WHERE title LIKE 'Delete the check %' ORDER BY substr(mission_id, 3)::int`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var m, g string
		if err = rows.Scan(&m, &g); err != nil {
			t.Fatal(err)
		}
		missions, goals = append(missions, m), append(goals, g)
	}
	return missions, goals
}

// A marked row proposes one goal a week, through ProposeGoal, with a
// check_value note; none while that goal is open; again once it is not.
func TestCheckValuePassProposesOncePerWeekAndNotWhileOpen(t *testing.T) {
	ctx := context.Background()
	pool := checkValueSetup(t)
	c := &Controller{DB: pool}
	if n, err := c.CheckValuePass(ctx); n != 0 || err != nil {
		t.Fatal("without a reader of main the pass proposes nothing", n, err)
	}
	main := func(context.Context, string) (string, error) { return strings.Repeat("a", 40), nil }
	propose := func(doc store.CheckValueDoc) int {
		t.Helper()
		n, err := c.checkValuePropose(ctx, doc, main)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := propose(checkValueDoc("2026-W44", "2026-W45", false)); n != 0 {
		t.Fatal("an unmarked row proposed", n)
	}
	doc := checkValueDoc("2026-W44", "2026-W45", true)
	if n := propose(doc); n != 1 {
		t.Fatal("the marked row proposed", n)
	}
	if n := propose(doc); n != 0 {
		t.Fatal("the same week proposed again", n)
	}
	missions, goals := checkValueGoals(t, pool)
	if len(missions) != 1 {
		t.Fatal("goals", missions)
	}
	for _, want := range []string{"the check Review (review)", "since 2026-W41", "- 2026-W44: ran 3, caught 0, unique 0, noise 0", "- 2026-W41:"} {
		if !strings.Contains(goals[0], want) {
			t.Fatalf("the goal lacks %q:\n%s", want, goals[0])
		}
	}
	if strings.Contains(goals[0], "2026-W45") {
		t.Fatalf("the goal lists the partial week:\n%s", goals[0])
	}
	var needs string
	var noted int
	if err := pool.QueryRow(ctx, `SELECT COALESCE(scope_json->>'needs_owner', '') FROM agentrt.mission WHERE mission_id = $1`, missions[0]).Scan(&needs); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'controller' AND event_type = 'note'
	  AND body_json->'check_value'->>'key' = 'review' AND body_json->'check_value'->>'week' = '2026-W44'
	  AND body_json->'check_value'->>'mission' = $1`, missions[0]).Scan(&noted); err != nil {
		t.Fatal(err)
	}
	if noted != 1 {
		t.Fatal("notes", noted)
	}
	if needs != checkValueNeedsOwner {
		t.Fatalf("needs_owner %q", needs)
	}
	// The next week, the goal still open: none.
	next := checkValueDoc("2026-W45", "2026-W46", true)
	if n := propose(next); n != 0 {
		t.Fatal("proposed while the goal is open", n)
	}
	if open, err := store.CheckValueOpenGoal(ctx, pool, "review"); err != nil || open != missions[0] {
		t.Fatal("open goal", open, err)
	}
	// Once it is stopped, the row's next marked week proposes again.
	if err := store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", missions[0], "stopped", "test") }); err != nil {
		t.Fatal(err)
	}
	if n := propose(next); n != 1 {
		t.Fatal("after the goal stopped", n)
	}
	if missions, _ = checkValueGoals(t, pool); len(missions) != 2 {
		t.Fatal("goals", missions)
	}
	if open, err := store.CheckValueOpenGoal(ctx, pool, "review"); err != nil || open != missions[1] {
		t.Fatal("open goal", open, err)
	}
}
