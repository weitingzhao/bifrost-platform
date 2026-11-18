package eventlog_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store" // the vocabulary and the seed, in the test
)

// Origin: 2026-11-16, the ledger got reads of its own (Last, Has, HasBody)
// so that other packages stop naming agentrt.event in their SQL. This is the
// ledger's contract test for them, from outside the package: each answer on
// a stream that holds what is asked, and its positive control beside it (a
// stream that does not, a type that does not match, a body one key apart).
func TestLedgerReads(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	const stream = "contract:reads"
	err := eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, ok, err := eventlog.Last(ctx, tx, stream); err != nil || ok {
			t.Fatalf("Last of an empty stream: ok=%v err=%v", ok, err)
		}
		for _, text := range []string{"first", "second"} {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: "plan_step_marked_done", Actor: "controller",
				Body: map[string]any{"text": text, "manual": "m"}}); err != nil {
				return err
			}
		}
		last, ok, err := eventlog.Last(ctx, tx, stream)
		if err != nil || !ok || last.Type != "plan_step_marked_done" || last.Body["text"] != "second" || last.Seq != 2 || last.Actor != "controller" || last.ID == 0 {
			t.Fatalf("Last = %+v ok=%v err=%v", last, ok, err)
		}
		for _, c := range []struct {
			name, stream, typ string
			text              map[string]string
			want              bool
		}{
			{"the text of an event of the stream", stream, "plan_step_marked_done", map[string]string{"text": "first"}, true},
			{"any type", stream, "", map[string]string{"text": "second"}, true},
			{"no keys asked", stream, "plan_step_marked_done", nil, true},
			{"another text", stream, "plan_step_marked_done", map[string]string{"text": "third"}, false},
			{"one key apart", stream, "plan_step_marked_done", map[string]string{"text": "first", "manual": "x"}, false},
			{"a key the body does not have", stream, "plan_step_marked_done", map[string]string{"missing": ""}, false},
			{"another type", stream, "policy_alert", map[string]string{"text": "first"}, false},
			{"another stream", "contract:other", "plan_step_marked_done", map[string]string{"text": "first"}, false},
		} {
			got, err := eventlog.Has(ctx, tx, c.stream, c.typ, c.text)
			if err != nil || got != c.want {
				t.Errorf("Has, %s: %v %v, want %v", c.name, got, err, c.want)
			}
		}
		for _, c := range []struct {
			name, typ, node string
			doc             map[string]any
			want            bool
		}{
			{"a body that contains the document", "plan_step_marked_done", "", map[string]any{"text": "first", "manual": "m"}, true},
			{"one key apart", "plan_step_marked_done", "", map[string]any{"text": "first", "manual": "x"}, false},
			{"another value", "plan_step_marked_done", "", map[string]any{"text": "third"}, false},
			{"a plan node the events are not of", "plan_step_marked_done", "R1", map[string]any{"text": "first"}, false},
			{"another type", "policy_alert", "", map[string]any{"text": "first"}, false},
		} {
			got, err := eventlog.HasBody(ctx, tx, c.typ, c.node, c.doc)
			if err != nil || got != c.want {
				t.Errorf("HasBody, %s: %v %v, want %v", c.name, got, err, c.want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Origin: 2026-11-16, the read shape is the contract other packages read
// the ledger by. It is held to the table the migrations build: a column
// added, dropped, retyped or made nullable in a migration without the
// declaration following fails here, and the declaration cannot name a
// column the table does not have.
func TestReadShapeIsTheTable(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	conn := db.Connect(t, db.Owner)
	rows, err := conn.Query(ctx, `SELECT column_name, data_type, is_nullable = 'YES' FROM information_schema.columns
	  WHERE table_schema = 'agentrt' AND table_name = 'event' ORDER BY ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var table []eventlog.Column
	for rows.Next() {
		var c eventlog.Column
		if err = rows.Scan(&c.Name, &c.Type, &c.Nullable); err != nil {
			t.Fatal(err)
		}
		table = append(table, c)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(table) == 0 {
		t.Fatal("the table has no columns: the check read nothing")
	}
	if len(table) != len(eventlog.ReadShape) {
		t.Fatalf("the table has %d columns, the read shape declares %d", len(table), len(eventlog.ReadShape))
	}
	for i, want := range table {
		got := eventlog.ReadShape[i]
		if got.Name != want.Name || got.Type != want.Type || got.Nullable != want.Nullable {
			t.Errorf("column %d: the table has %s %s nullable=%v, the read shape declares %s %s nullable=%v",
				i+1, want.Name, want.Type, want.Nullable, got.Name, got.Type, got.Nullable)
		}
		if got.Meaning == "" {
			t.Errorf("column %s is declared without a meaning", got.Name)
		}
	}
}
