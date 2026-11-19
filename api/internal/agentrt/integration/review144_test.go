package integration_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

func note(tx pgx.Tx) error {
	_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "note", Actor: "controller", Body: map[string]any{"text": "x"}})
	return err
}

// Review 144 A1: an event writes only the tables its type may write and only
// the rows it names; ids never change; state rows are never deleted.
func TestEventWritesOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	// Two controls through the contract's own writer (RequestControl), the ids
	// it minted standing in for the fixed ones the direct writes had.
	c1, err := coord.RequestControl(ctx, w.rt, wr("controller"), "k-1", "global", "", "pause", "x")
	ok(t, err)
	c2, err := coord.RequestControl(ctx, w.rt, wr("controller"), "k-2", "global", "", "stop", "y")
	ok(t, err)
	id1, id2 := c1["control_id"].(string), c2["control_id"].(string)
	var m1, m2 string
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		if m1, err = coord.PostMessage(ctx, tx, "controller", "exec:codex", "note", []byte("1")); err != nil {
			return err
		}
		m2, err = coord.PostMessage(ctx, tx, "controller", "exec:codex", "note", []byte("2"))
		return err
	}))

	// A note clears every control: refused by type.
	wantErr(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := note(tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.control SET cleared_at = clock_timestamp(), cleared_by_actor_id = 'owner' WHERE cleared_at IS NULL`)
		return err
	}), "does not write agentrt.control")
	// control_cleared for c-1 also clears c-2: refused by name.
	wantErr(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "control", Type: "control_cleared", Actor: "owner",
			Body: map[string]any{"control_id": id1}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.control SET cleared_at = clock_timestamp(), cleared_by_actor_id = 'owner' WHERE cleared_at IS NULL`)
		return err
	}), "does not name agentrt.control row "+id2)
	// message_acked for m1 acks m2: refused by name.
	wantErr(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "mbox:exec:codex", Type: "message_acked", Actor: "exec:codex",
			Body: map[string]any{"message_id": m1}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.message SET acked_at = clock_timestamp() WHERE message_id = $1`, m2)
		return err
	}), "does not name agentrt.message row")
	// An actor added under a note.
	wantErr(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := note(tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('seat:x', 'seat', 'x')`)
		return err
	}), "does not write agentrt.actor")
	// The owner cannot delete or truncate state rows.
	owner := w.db.Connect(t, w.db.Owner)
	_, err = owner.Exec(ctx, `DELETE FROM agentrt.control WHERE control_id = $1`, id2)
	wantErr(t, err, "end by state")
	_, err = owner.Exec(ctx, `TRUNCATE agentrt.control`)
	wantErr(t, err, "")
	// The right event, the right row: goes through.
	_, err = coord.AckMessages(ctx, w.rt, wr("exec:codex"), []string{m2})
	ok(t, err)
}

// T23a for all eight append-only tables, by the owner (the runtime role is
// stopped earlier by missing privileges, which proves less).
func TestAppendOnlyTablesRefuseTheOwner(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, planChange(w, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT current_setting('agentrt.event_id')::bigint`).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.plan_node_binding (plan_node_id, bound_kind, bound_ref, event_id) VALUES ($1,'approval','17',$2)`, s1, id)
		return err
	}))
	owner := w.db.Connect(t, w.db.Owner)
	for _, q := range []string{
		`INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature, effective_at, expires_at)
		 VALUES ('job:implementer', 1, '{}', '{}', 'initial', 'owner', 'sig', now(), now() + interval '90 days')`,
		// evidence and action_record stand where budget_ledger and
		// capability_score stood until 0036 dropped them (2026-11-18).
		`INSERT INTO agentrt.evidence (repo, unit, task, input_key, result, scope, inputs, statement, commit_sha, run_name, run_uid)
		 VALUES ('bifrost-platform', 'api', 'test', repeat('a', 64), 'passed', 'full', '{}', '{}', repeat('b', 40), 'run', 'uid')`,
		`INSERT INTO agentrt.action_record (seat, action, reason, regret, regret_state, law, outcome)
		 VALUES ('seat', 'act', 'why', '', 'undeclared', 'law', 'done')`,
	} {
		_, err := owner.Exec(ctx, q)
		ok(t, err)
	}
	for _, tbl := range []string{"event", "artifact", "artifact_blob", "evidence",
		"action_record", "rule", "plan_node_binding", "schema_migration"} {
		var n int
		ok(t, owner.QueryRow(ctx, `SELECT count(*) FROM agentrt.`+tbl).Scan(&n))
		if n == 0 {
			t.Fatalf("agentrt.%s is empty; the guard would not fire", tbl)
		}
		_, err := owner.Exec(ctx, `UPDATE agentrt.`+tbl+` SET `+firstColumn(tbl)+` = `+firstColumn(tbl))
		wantErr(t, err, "append only")
		_, err = owner.Exec(ctx, `DELETE FROM agentrt.`+tbl)
		wantErr(t, err, "append only")
	}
}

func firstColumn(tbl string) string {
	switch tbl {
	case "event":
		return "event_type"
	case "artifact", "artifact_blob":
		return "artifact_id"
	case "evidence":
		return "repo"
	case "action_record":
		return "seat"
	case "rule":
		return "body_text"
	case "plan_node_binding":
		return "bound_ref"
	}
	return "sha256"
}

// T23b beyond an edited body: a deleted tail, a deleted middle event and an
// edited head are each found.
func TestTamperShapesAreFound(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for i := range 3 {
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "audit", Type: "note", Actor: "controller", Body: map[string]any{"text": string(rune('a' + i))}})
			return err
		}))
	}
	admin := w.db.Connect(t, w.db.Admin)
	tamper := func(q string) {
		for _, s := range []string{`ALTER TABLE agentrt.event DISABLE TRIGGER USER`, `ALTER TABLE agentrt.event_stream DISABLE TRIGGER USER`, q,
			`ALTER TABLE agentrt.event ENABLE TRIGGER USER`, `ALTER TABLE agentrt.event_stream ENABLE TRIGGER USER`} {
			_, err := admin.Exec(ctx, s)
			ok(t, err)
		}
	}
	verify := func() *eventlog.ChainBreak {
		_, err := eventlog.VerifyStream(ctx, w.rt, "audit")
		var br *eventlog.ChainBreak
		if !errors.As(err, &br) {
			t.Fatalf("verify = %v, want a chain break", err)
		}
		return br
	}
	if _, err := eventlog.VerifyStream(ctx, w.rt, "audit"); err != nil {
		t.Fatal(err)
	}
	tamper(`UPDATE agentrt.event_stream SET last_sha256 = repeat('0', 64) WHERE event_stream_id = 'audit'`)
	_ = verify()
	tamper(`UPDATE agentrt.event_stream SET last_sha256 = (SELECT sha256 FROM agentrt.event WHERE event_stream_id = 'audit' AND seq = 3) WHERE event_stream_id = 'audit'`)
	tamper(`DELETE FROM agentrt.event WHERE event_stream_id = 'audit' AND seq = 2`)
	if br := verify(); br.Seq != 3 {
		t.Fatalf("deleted middle: break at %d, want 3", br.Seq)
	}
	tamper(`DELETE FROM agentrt.event WHERE event_stream_id = 'audit' AND seq = 3`)
	_ = verify()
}

// T32 extras: a done rule naming a question needs a binding; an item cannot
// sit under an item; moving a task a done rule names fails with that reason.
func TestPlanBindingsAndShapes(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	wantErr(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: "R2:2.1x", Parent: item, Kind: "item", Label: "x", Title: "x", Ordinal: 9, Root: root})
		return err
	}), "cannot sit under")
	rule := `{"question_answered":{"question":"Q3","option":"A"}}`
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, s2, rule)
		return err
	}), "names question Q3")
	ok(t, planChange(w, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT current_setting('agentrt.event_id')::bigint`).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.plan_node_binding (plan_node_id, bound_kind, bound_ref, event_id) VALUES ($1,'question','Q3',$2)`, s2, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, s2, rule)
		return err
	}))

}

// T19: a failed commit takes its event with it.
func TestFailedCommitLeavesNoEvent(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	var before int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&before))
	err := eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		// The row the event names does not exist: a mission never created (a
		// task's id is no longer checked, its table is gone since 0020).
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "mission:M-1", Type: "mission_state_changed", Actor: "controller",
			MissionID: "M-99", Body: map[string]any{"from": "proposed", "to": "active"}})
		if err != nil {
			return err
		}
		return nil
	})
	wantErr(t, err, "foreign key")
	var after int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&after))
	if after != before {
		t.Fatalf("events went from %d to %d rows after a failed commit", before, after)
	}
}

// T20: the writer's connection is killed in the middle of a stream of
// writes; writing resumes on a new connection and the chain stays whole.
//
// Serial: it times 200 writes after a killed connection against a budget (review 322 B4).
func TestKilledConnectionKeepsTheChain(t *testing.T) {
	w := newWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	admin := w.db.Connect(t, w.db.Admin)
	write := func(i int) error {
		return eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "load", Type: "note", Actor: "controller", Body: map[string]any{"text": string(rune('a' + i%26))}})
			return err
		})
	}
	killed := false
	written := 0
	start := time.Now()
	for i := 0; written < 200 && time.Since(start) < 10*time.Second; i++ {
		if i == 50 && !killed {
			_, err := admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = 'platform_agentrt_dev' AND datname = current_database()`)
			ok(t, err)
			killed = true
		}
		if err := write(i); err == nil {
			written++
		}
	}
	if written < 200 {
		t.Fatalf("only %d writes in 10 seconds after the kill", written)
	}
	n, err := eventlog.VerifyStream(ctx, pool, "load")
	ok(t, err)
	if n != int64(written) {
		t.Fatalf("stream has %d events, %d writes succeeded", n, written)
	}
}
