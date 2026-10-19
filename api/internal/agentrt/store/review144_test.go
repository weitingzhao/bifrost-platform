package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func note(tx pgx.Tx) error {
	_, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "note", Actor: "controller", Body: map[string]any{"text": "x"}})
	return err
}

// Review 144 A1: an event writes only the tables its type may write and only
// the rows it names; ids never change; state rows are never deleted.
func TestEventWritesOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.SetControl(ctx, tx, "controller", "c-1", "global", "", "pause", "x"); err != nil {
			return err
		}
		return store.SetControl(ctx, tx, "controller", "c-2", "global", "", "stop", "y")
	}))
	var m1, m2 string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		if m1, err = store.PostMessage(ctx, tx, "controller", "exec:codex", "note", []byte("1")); err != nil {
			return err
		}
		m2, err = store.PostMessage(ctx, tx, "controller", "exec:codex", "note", []byte("2"))
		return err
	}))

	// A note clears every control: refused by type.
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := note(tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.control SET cleared_at = clock_timestamp(), cleared_by_actor_id = 'owner' WHERE cleared_at IS NULL`)
		return err
	}), "does not write agentrt.control")
	// control_cleared for c-1 also clears c-2: refused by name.
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "control", Type: "control_cleared", Actor: "owner",
			Body: map[string]any{"control_id": "c-1"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.control SET cleared_at = clock_timestamp(), cleared_by_actor_id = 'owner' WHERE cleared_at IS NULL`)
		return err
	}), "does not name agentrt.control row c-2")
	// message_acked for m1 acks m2: refused by name.
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "mbox:exec:codex", Type: "message_acked", Actor: "exec:codex",
			Body: map[string]any{"message_id": m1}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.message SET acked_at = clock_timestamp() WHERE message_id = $1`, m2)
		return err
	}), "does not name agentrt.message row")
	// An actor added under a note.
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := note(tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('seat:x', 'seat', 'x')`)
		return err
	}), "does not write agentrt.actor")
	// A plan change that moves nothing but changes a task's state.
	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err = tx.Exec(ctx, `UPDATE agentrt.task SET state = 'ready' WHERE task_id = $1`, t1)
		return err
	}), "changes nothing else")
	// The owner cannot delete or truncate state rows.
	owner := w.db.Connect(t, w.db.Owner)
	_, err = owner.Exec(ctx, `DELETE FROM agentrt.control WHERE control_id = 'c-2'`)
	wantErr(t, err, "end by state")
	_, err = owner.Exec(ctx, `TRUNCATE agentrt.control`)
	wantErr(t, err, "")
	// The right event, the right row: goes through.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.AckMessage(ctx, tx, "exec:codex", m2) }))
}

// Review 144 A3: state is written only under READ COMMITTED.
func TestRepeatableReadWritesAreRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a, err := w.task(t, "M-1", s3, "", "implement")
	ok(t, err)
	b, err := w.task(t, "M-2", s2, "", "implement")
	ok(t, err)
	tx, err := w.rt.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = store.AddDependency(ctx, tx, "controller", "M-1", a, b)
	wantErr(t, err, "READ COMMITTED")
}

// Review 144 A2: an off-plan review created while its off-plan subject is
// being moved onto a step, in both orders.
func TestSubjectLockSerializesMoveAndReview(t *testing.T) {
	t.Parallel()
	for _, moveFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "move_first", false: "review_first"}[moveFirst], func(t *testing.T) {
			w := newWorld(t)
			subj, err := w.task(t, "M-1", "", "", "implement")
			ok(t, err)
			other := w.db.Connect(t, w.db.Runtime)
			move := func(tx pgx.Tx) error {
				if _, err = store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
					MissionID: "M-1", PlanNode: s2, TaskID: subj, Body: map[string]any{"change": "migrate"}}); err != nil {
					return err
				}
				_, err = tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2, off_plan = false, off_plan_reason = NULL WHERE task_id = $1`, subj, s2)
				return err
			}
			review := func(tx pgx.Tx) error {
				_, err = store.ProposeTask(ctx, tx, "controller", store.Task{MissionID: "M-1", Subject: subj, Role: "review",
					OffPlanReason: "review of an off-plan task", Class: "go", Title: "r", SpecArtifact: w.spec, Effect: "none"})
				return err
			}
			first, second := move, review
			if !moveFirst {
				first, second = review, move
			}
			tx1, err := w.rt.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			ok(t, err)
			defer func() { _ = tx1.Rollback(ctx) }()
			ok(t, first(tx1))
			done := make(chan error, 1)
			go func() { done <- store.InTx(ctx, other, second) }()
			select {
			case err := <-done:
				t.Fatalf("second transaction did not wait for the subject lock: %v", err)
			case <-time.After(300 * time.Millisecond):
			}
			ok(t, tx1.Commit(ctx))
			wantErr(t, <-done, "sit")
			var bad int
			ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task t JOIN agentrt.task s ON s.task_id = t.subject_task_id
			  WHERE t.plan_node_id IS DISTINCT FROM s.plan_node_id OR t.off_plan <> s.off_plan`).Scan(&bad))
			if bad != 0 {
				t.Fatalf("%d tasks sit apart from their subject", bad)
			}
		})
	}
}

// Review 144 A5: an attempt keeps its task and its result once written; an
// accepting verification needs a persisted result.
func TestAttemptKeepsItsResult(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	t2, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	var a1, a2 string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		if a1, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", t1, "exec:codex", "", 1, time.Minute); err != nil {
			return err
		}
		a2, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", t2, "exec:codex", "", 1, time.Minute)
		return err
	}))
	persist := func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "attempt_persisted", Actor: "controller",
			TaskID: t1, AttemptID: a1, Body: map[string]any{"pushed_commit": "2333e00753f91c74c8b2f4d693a803f8e89e7892"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.attempt SET persisted_at = clock_timestamp(), pushed_ref = 'refs/heads/agent/t/a1',
		  pushed_commit = '2333e00753f91c74c8b2f4d693a803f8e89e7892' WHERE attempt_id = $1`, a1)
		return err
	}
	ok(t, store.InTx(ctx, w.rt, persist))
	for q, want := range map[string]string{
		`UPDATE agentrt.attempt SET persisted_at = NULL WHERE attempt_id = $1`:             "written once",
		`UPDATE agentrt.attempt SET pushed_commit = repeat('a', 40) WHERE attempt_id = $1`: "written once",
		`UPDATE agentrt.attempt SET attempt_no = 7 WHERE attempt_id = $1`:                  "keeps its task",
	} {
		wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "attempt_persisted", Actor: "controller",
				TaskID: t1, AttemptID: a1, Body: map[string]any{"pushed_commit": "2333e00753f91c74c8b2f4d693a803f8e89e7892"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, q, a1)
			return err
		}), want)
	}
	evidence := func(tx pgx.Tx) (string, error) {
		return store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte("ok"))
	}
	verify := func(task, attempt string) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			ev, err := evidence(tx)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
			  evidence_artifact_id, accepts_attempt) VALUES ($1,$2,$3,'deterministic','pass',$4,true)`,
				store.NewVerificationID(), task, attempt, ev)
			return err
		})
	}
	wantErr(t, verify(t2, a2), "before its result is persisted")
	wantErr(t, verify(t2, a1), "is not an attempt of task")
	ok(t, verify(t1, a1))
	wantErr(t, verify(t1, a1), "verification_one_accept")
}

// T23a for all nine append-only tables, by the owner (the runtime role is
// stopped earlier by missing privileges, which proves less).
func TestAppendOnlyTablesRefuseTheOwner(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	var a1 string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		if a1, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", t1, "exec:codex", "", 1, time.Minute); err != nil {
			return err
		}
		if _, err = store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "attempt_persisted", Actor: "controller",
			TaskID: t1, AttemptID: a1, Body: map[string]any{"pushed_commit": "2333e00753f91c74c8b2f4d693a803f8e89e7892"}}); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.attempt SET persisted_at = clock_timestamp() WHERE attempt_id = $1`, a1); err != nil {
			return err
		}
		ev, err := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte("ok"))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict, evidence_artifact_id)
		  VALUES ($1,$2,$3,'deterministic','pass',$4)`, store.NewVerificationID(), t1, a1, ev); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.budget_ledger (attempt_id, unit, amount, measured) VALUES ($1, 'wall_s', 12, true)`, a1)
		return err
	}))
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
	} {
		_, err := owner.Exec(ctx, q)
		ok(t, err)
	}
	ok(t, store.InTx(ctx, owner, func(tx pgx.Tx) error {
		ev, err := store.Append(ctx, tx, store.Event{Stream: "rule", Type: "rule_signed", Actor: "owner",
			Body: map[string]any{"rule_id": "job:implementer", "rule_version": 1, "job_ids": []string{"implementer"}}})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('implementer','dev','job:implementer',1)`); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model, tool_version,
		  job_id, task_class, core, source, window_start, window_end, attempts, first_pass, accepted, rework, score, computed_from_event_id)
		  VALUES ('s1','exec:codex','openai','m','1','implementer','go',false,'exam', now() - interval '1 day', now(), 1,1,1,0,0.5,$1)`, ev.EventID)
		return err
	}))
	for _, tbl := range []string{"event", "artifact", "artifact_blob", "verification", "budget_ledger",
		"capability_score", "rule", "plan_node_binding", "schema_migration"} {
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
	case "verification":
		return "verdict"
	case "budget_ledger":
		return "unit"
	case "capability_score":
		return "score"
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
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "audit", Type: "note", Actor: "controller", Body: map[string]any{"text": string(rune('a' + i))}})
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
	verify := func() *store.ChainBreak {
		_, err := store.VerifyStream(ctx, w.rt, "audit")
		var br *store.ChainBreak
		if !errors.As(err, &br) {
			t.Fatalf("verify = %v, want a chain break", err)
		}
		return br
	}
	if _, err := store.VerifyStream(ctx, w.rt, "audit"); err != nil {
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
	wantErr(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R2:2.1x", Parent: item, Kind: "item", Label: "x", Title: "x", Ordinal: 9, Root: root})
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

	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	ok(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = jsonb_build_object('task_accepted', $2::text) WHERE plan_node_id = $1`, s1, t1)
		return err
	}))
	wantErr(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2 WHERE task_id = $1`, t1, s3)
		return err
	}), "names task")
}

// T19: a failed commit takes its outbox row with it.
func TestFailedCommitLeavesNoOutboxRow(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	var before int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.outbox`).Scan(&before))
	err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		ev, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_proposed", Actor: "controller",
			MissionID: "M-1", TaskID: store.NewTaskID(), Body: map[string]any{"title": "never created", "role": "other"}})
		if err != nil {
			return err
		}
		return store.Enqueue(ctx, tx, ev.EventID, store.TopicWake, map[string]any{"kind": "task_ready"})
	})
	wantErr(t, err, "foreign key")
	var after int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.outbox`).Scan(&after))
	if after != before {
		t.Fatalf("outbox went from %d to %d rows after a failed commit", before, after)
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
		return store.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "load", Type: "note", Actor: "controller", Body: map[string]any{"text": string(rune('a' + i%26))}})
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
	n, err := store.VerifyStream(ctx, pool, "load")
	ok(t, err)
	if n != int64(written) {
		t.Fatalf("stream has %d events, %d writes succeeded", n, written)
	}
}
