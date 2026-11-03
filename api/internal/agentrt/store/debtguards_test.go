package store_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// de is an event on a debt's stream naming the debt.
func de(debt, typ, actor string, body map[string]any) store.Event {
	body["debt_id"] = debt
	return store.Event{Stream: store.DebtStream(debt), Type: typ, Actor: actor, Body: body}
}

// run writes ev and then the statements in one transaction as the runtime
// role, past the store; an event the store itself refuses is the case's own
// mistake, not the database's refusal.
func (dw *debtWorld) run(t *testing.T, ev store.Event, stmts ...string) error {
	t.Helper()
	var appendErr error
	err := store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, ev); err != nil {
			appendErr = err
			return err
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	if appendErr != nil {
		t.Fatalf("the case's event does not append: %v", appendErr)
	}
	return err
}

func debtTextSQL(id int64, debt, kind, actor string) string {
	return fmt.Sprintf(`INSERT INTO agentrt.debt_text (debt_text_id, debt_id, text_kind, body, sha256, created_by_actor_id)
	  OVERRIDING SYSTEM VALUE VALUES (%d, '%s', '%s', 'b', encode(sha256('b'), 'hex'), '%s')`, id, debt, kind, actor)
}

// debtRunSQL is a run of debt's current spec (the database computes its
// hash), naming at_commits; a checker's record names the record it checks
// (checks, 0 for none) and keeps its run time.
func debtRunSQL(id int64, debt, runner, result, actor, checker, artifact string, checks int64) string {
	opt := func(s string) string {
		if s == "" {
			return "NULL"
		}
		return "'" + s + "'"
	}
	ran, ref := "now()", "NULL"
	if checks != 0 {
		ran = fmt.Sprintf("COALESCE((SELECT ran_at FROM agentrt.debt_acceptance_run WHERE debt_acceptance_run_id = %d), now())", checks)
		ref = fmt.Sprint(checks)
	}
	return fmt.Sprintf(`INSERT INTO agentrt.debt_acceptance_run (debt_acceptance_run_id, debt_id, runner, spec_sha256, result, at_commits, ran_at,
	    evidence_artifact_id, checked_by_actor_id, checks_run_id, created_by_actor_id) OVERRIDING SYSTEM VALUE
	  SELECT %d, '%s', '%s', agentrt.debt_spec_sha(acceptance_json), '%s', '{"repo-a": "0123abc"}', %s, %s, %s, %s, '%s'
	    FROM agentrt.debt WHERE debt_id = '%s'`, id, debt, runner, result, ran, opt(artifact), opt(checker), ref, actor, debt)
}

func debtMoveSQL(debt, state, conclusion string) string {
	c := "NULL"
	if conclusion != "" {
		c = "'" + conclusion + "'"
	}
	return fmt.Sprintf(`UPDATE agentrt.debt SET state = '%s', conclusion = %s, state_since = clock_timestamp(), version = version + 1
	  WHERE debt_id = '%s'`, state, c, debt)
}

// fileWithSpec files a debt with a manual acceptance item and one part,
// so it flows automatically and runs can name its spec.
func (dw *debtWorld) fileWithSpec(t *testing.T, title string) string {
	t.Helper()
	f := debtFiling(title)
	f.Repos = []string{"repo-a"}
	f.Acceptance = []any{map[string]any{"kind": "manual", "say": "look", "expect": "fine"}}
	out, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), f)
	ok(t, err)
	return out["debt_id"].(string)
}

// The debt tables of 0011: state tables, then append-only ones.
var debtStateTables = []string{"debt", "debt_counter", "debt_edge", "debt_ref", "debt_work", "debt_part", "debt_part_order",
	"debt_decision", "ratchet", "accepted_gap"}
var debtAppendTables = []string{"debt_text", "debt_evidence", "debt_evidence_check", "debt_part_landing", "debt_acceptance_run"}

// Every guard 0011 adds refuses its counterexample, written past the store
// as the runtime role, and lets its allowed counterpart through (§2.2, §10
// row 5.8-3a): on every debt table, a write with no event and one by the
// import; a debt written by another table's event or the fields by another
// debt event; an external agent or a host; the Owner's events (sign-off,
// gap) and the runtime's (CI and Prometheus results, a move by a rule) by
// the crew; a reopen by the runtime; an import or Owner run by the crew; a
// landing the crew certifies itself; a run of no spec or of another one; a
// manual result without a second writer over the author's record; a state
// moved by the wrong event; an automatic flow without a spec and a part; a
// ref to what is not a step. A: the crew's, with a spec and a part; B: the
// crew's, with neither; S: imported awaiting the Owner's sign-off.
func TestDebtGuardMatrix(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	A, B := dw.fileWithSpec(t, "the guard matrix"), dw.mustFile(t, "stg", "no spec")
	const S, crew, other = "TDS-900", "thread:crew-1", "thread:crew-2"
	dw.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('agent:x', 'agent', 'x'), ('host:h9', 'host', 'h9'),
	  ('thread:crew-2', 'thread', 'crew 2')`)
	importAwaiting(t, dw, S, "pass", 0, 900090)
	var proof string
	var part int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT debt_part_id FROM agentrt.debt_part WHERE debt_id = $1`, A).Scan(&part))
	ok(t, store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		var err error
		proof, err = store.PutArtifact(ctx, tx, crew, "report", "text/markdown", []byte("checked by hand"))
		return err
	}))
	// An author's record that exists, for the case of another record naming it.
	ok(t, dw.run(t, de(A, "debt_acceptance_ran", crew, map[string]any{"debt_acceptance_run_id": 900050, "runner": "manual",
		"result": "unknown"}), debtRunSQL(900050, A, "manual", "unknown", crew, "", proof, 0)))
	textAdded := func(id int64, actor string) store.Event {
		return de(A, "debt_text_added", actor, map[string]any{"debt_text_id": id, "text_kind": "note", "sha256": "3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d"})
	}
	ran := func(id int64, debt, runner, result, actor string) store.Event {
		return de(debt, "debt_acceptance_ran", actor, map[string]any{"debt_acceptance_run_id": id, "runner": runner, "result": result})
	}
	landed := func(actor string) store.Event {
		return de(A, "debt_part_landed", actor, map[string]any{"debt_part_id": part, "debt_part_landing_id": 900007,
			"commit": "0123456789abcdef0123456789abcdef01234567", "via": "runtime_landing"})
	}
	landing := fmt.Sprintf(`INSERT INTO agentrt.debt_part_landing (debt_part_landing_id, debt_part_id, commit_sha, via, created_by_actor_id)
	  OVERRIDING SYSTEM VALUE VALUES (900007, %d, '0123456789abcdef0123456789abcdef01234567', 'runtime_landing', '%s')`, part, crew)
	legacy := store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller", Body: map[string]any{"source": "test"}}
	movedNot := de(A, "debt_work_linked", crew, map[string]any{"debt_work_id": 900096, "from": "open", "to": "doing"})
	movedNot.MissionID = "M-1"
	retitle := fmt.Sprintf(`UPDATE agentrt.debt SET title = 'renamed', version = version + 1 WHERE debt_id = '%s'`, A)

	type refusal struct {
		name, refusal string
		ev            store.Event // zero: no event at all
		stmt          string      // statements apart by a NUL, in order
	}
	var cases []refusal
	for _, tbl := range append(slices.Clone(debtStateTables), debtAppendTables...) {
		insert := `INSERT INTO agentrt.` + tbl + ` DEFAULT VALUES`
		cases = append(cases, refusal{tbl + " with no event", "needs SET LOCAL agentrt.event_id", store.Event{}, insert},
			refusal{tbl + " by the import", "a legacy_imported event does not write agentrt." + tbl, legacy, insert})
	}
	cases = append(cases, []refusal{
		{"a filing its event misstates", "debt_filed records the debt it files", de("TDS-901", "debt_filed", crew, map[string]any{
			"source": "file", "project": "crew", "priority": "P3", "state": "awaiting_signoff", "flow": "manual"}),
			insertDebtSQL("TDS-901", "open", crew)},
		{"a debt by a text's event", "a debt_text_added event does not write agentrt.debt", textAdded(900001, crew), retitle},
		{"a move its event does not record", "is not the one its debt_signoff_rejected event records",
			de(S, "debt_signoff_rejected", "owner", map[string]any{"debt_text_ids": []any{900095}}),
			debtTextSQL(900095, S, "reject_reason", "owner") + "\x00" + debtMoveSQL(S, "doing", "")},
		{"an event recording a move not made", "records a move of " + A + " from open to doing it did not make", movedNot,
			fmt.Sprintf(`INSERT INTO agentrt.debt_work (debt_work_id, debt_id, mission_id, started_at) OVERRIDING SYSTEM VALUE
			  VALUES (900096, '%s', 'M-1', now())`, A)},
		{"a manual record naming another", "check constraint", ran(900097, A, "manual", "unknown", crew),
			debtRunSQL(900097, A, "manual", "unknown", crew, "", proof, 900050)},
		{"the runtime writes a transition", "permission denied", store.Event{}, fmt.Sprintf(`INSERT INTO agentrt.debt_transition
		  (debt_id, event_id, from_state, to_state) VALUES ('%s', 1, 'awaiting_signoff', 'doing')`, S)},
		{"a filing that files nothing", "created no debt", de(A, "debt_filed", crew, map[string]any{"source": "import", "project": "crew",
			"priority": "P3", "state": "awaiting_signoff", "flow": "manual"}), `SELECT 1`},
		{"the birth moved", "keeps when and by whom it was filed", de(A, "debt_edited", crew, map[string]any{"fields": []string{"title"}}),
			fmt.Sprintf(`UPDATE agentrt.debt SET birth_event_id = 1, title = 'x', version = version + 1 WHERE debt_id = '%s'`, A)},
		{"the fields by another debt event", "change by debt_edited", de(A, "debt_flow_set", crew, map[string]any{"flow": "auto"}), retitle},
		{"an external agent writes", "agent:x does not write a debt_text_added event", textAdded(900001, "agent:x"), debtTextSQL(900001, A, "note", "agent:x")},
		{"a host writes", "host:h9 does not write a debt_text_added event", textAdded(900001, "host:h9"), debtTextSQL(900001, A, "note", "host:h9")},
		{"the crew signs off", "thread:crew-1 does not write a debt_signed_off event", de(S, "debt_signed_off", crew, map[string]any{}),
			debtMoveSQL(S, "closed", "signed_off")},
		{"the crew records the CI's result", "thread:crew-1 does not write a debt_acceptance_ran event",
			ran(900002, A, "ci_evidence", "pass", crew), debtRunSQL(900002, A, "ci_evidence", "pass", crew, "", "", 0)},
		{"the crew records an import's result", "thread:crew-1 does not write a debt_acceptance_ran event",
			ran(900002, A, "imported", "pass", crew), debtRunSQL(900002, A, "imported", "pass", crew, "", "", 0)},
		{"the crew records the Owner's run", "thread:crew-1 does not write a debt_acceptance_ran event",
			ran(900002, A, "owner", "pass", crew), debtRunSQL(900002, A, "owner", "pass", crew, "", "", 0)},
		{"the crew moves by a rule", "thread:crew-1 does not write a debt_state_changed event",
			de(A, "debt_state_changed", crew, map[string]any{"from": "open", "to": "doing", "by_rule": "r"}), debtMoveSQL(A, "doing", "")},
		{"the runtime reopens", "controller does not write a debt_reopened event", de(S, "debt_reopened", "controller", map[string]any{}),
			debtMoveSQL(S, "doing", "")},
		{"the crew certifies a landing", "only the runtime records", landed(crew), landing},
		{"a run of no spec", "of its current spec", ran(900002, B, "ci_evidence", "pass", "controller"),
			debtRunSQL(900002, B, "ci_evidence", "pass", "controller", "", "", 0)},
		{"a run of another spec", "of its current spec", ran(900002, A, "ci_evidence", "pass", "controller"),
			strings.Replace(debtRunSQL(900002, A, "ci_evidence", "pass", "controller", "", "", 0), "agentrt.debt_spec_sha(acceptance_json)", "repeat('0', 64)", 1)},
		{"a state by the wrong event", "does not move from open to closed (signed_off) by debt_state_changed",
			de(A, "debt_state_changed", crew, map[string]any{"from": "open", "to": "closed", "conclusion": "signed_off"}),
			debtMoveSQL(A, "closed", "signed_off")},
		{"an automatic flow with no spec", "flows automatically only with an acceptance spec and at least one part",
			de(B, "debt_flow_set", crew, map[string]any{"flow": "auto"}),
			fmt.Sprintf(`UPDATE agentrt.debt SET flow = 'auto', version = version + 1 WHERE debt_id = '%s'`, B)},
		{"a manual pass with no checker", "a checker who is not its author", ran(900003, A, "manual", "pass", crew),
			debtRunSQL(900003, A, "manual", "pass", crew, "", proof, 900004)},
		{"a manual pass its author checked", "a checker who is not its author", ran(900003, A, "manual", "pass", crew),
			debtRunSQL(900003, A, "manual", "pass", crew, crew, proof, 900004)},
		{"a manual pass over no record of its author", "checks the one record its author made", ran(900003, A, "manual", "pass", other),
			debtRunSQL(900003, A, "manual", "pass", crew, other, proof, 999999)},
	}...)
	for _, c := range cases {
		var err error
		if c.ev.Type == "" {
			_, err = dw.rt.Exec(ctx, c.stmt)
		} else {
			err = dw.run(t, c.ev, strings.Split(c.stmt, "\x00")...)
		}
		if err == nil {
			t.Errorf("%s: written", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.refusal) {
			t.Errorf("%s: %v, want %q", c.name, err, c.refusal)
		}
	}

	// The allowed counterparts, in order (each changes what the next sees).
	work := de(A, "debt_work_linked", crew, map[string]any{"debt_work_id": 900006, "from": "open", "to": "doing"})
	work.MissionID = "M-1"
	for _, c := range []struct {
		name  string
		ev    store.Event
		stmts []string
	}{
		{"the crew retitles by debt_edited", de(A, "debt_edited", crew, map[string]any{"fields": []string{"title"}}), []string{retitle}},
		{"the crew adds a text", textAdded(900001, crew), []string{debtTextSQL(900001, A, "note", crew)}},
		{"live work of a dev mission starts it", work, []string{fmt.Sprintf(`INSERT INTO agentrt.debt_work (debt_work_id, debt_id, mission_id,
		  started_at) OVERRIDING SYSTEM VALUE VALUES (900006, '%s', 'M-1', now())`, A), debtMoveSQL(A, "doing", "")}},
		{"the runtime records a landing", landed("controller"), []string{strings.Replace(landing, "'"+crew+"'", "'controller'", 1)}},
		{"the runtime records the CI's result", ran(900002, A, "ci_evidence", "pass", "controller"),
			[]string{debtRunSQL(900002, A, "ci_evidence", "pass", "controller", "", "", 0)}},
		{"the author records by hand", ran(900004, A, "manual", "unknown", crew), []string{debtRunSQL(900004, A, "manual", "unknown", crew, "", proof, 0)}},
		{"a second writer checks it", ran(900003, A, "manual", "pass", other), []string{debtRunSQL(900003, A, "manual", "pass", crew, other, proof, 900004)}},
		{"the Owner signs off", de(S, "debt_signed_off", "owner", map[string]any{"from": "awaiting_signoff", "to": "closed"}),
			[]string{debtMoveSQL(S, "closed", "signed_off")}},
	} {
		if err := dw.run(t, c.ev, c.stmts...); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}

	// A sign-off is reopened by the Owner, never by the crew (the crew
	// reopens only an automatic close, within seven days).
	reopen := func(actor string, text int64) error {
		return dw.run(t, de(S, "debt_reopened", actor, map[string]any{"debt_text_ids": []any{text}, "from": "closed", "to": "doing"}),
			debtTextSQL(text, S, "note", actor),
			debtMoveSQL(S, "doing", ""))
	}
	wantErr(t, reopen(crew, 900011), "only the Owner reopens it")
	ok(t, reopen("owner", 900012))
	// A ref pays by a step, named in its event's envelope as well.
	ref := func(node string) error {
		ev := de(A, "debt_ref_added", crew, map[string]any{"debt_ref_id": 900005, "ref_kind": "paid_by_step"})
		ev.PlanNode = node
		return dw.run(t, ev, fmt.Sprintf(`INSERT INTO agentrt.debt_ref (debt_ref_id, debt_id, ref_kind, plan_node_id, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES (900005, '%s', 'paid_by_step', '%s', '%s')`, A, node, crew))
	}
	wantErr(t, ref(item), "a debt is paid by a step")
	ok(t, ref(s2))
	// A gap is accepted by the Owner alone; a ratchet is the crew's.
	gap := func(actor string) error {
		return dw.run(t, store.Event{Stream: "gap:AG-1", Type: "gap_accepted", Actor: actor, Body: map[string]any{"accepted_gap_id": "AG-1"}},
			`INSERT INTO agentrt.accepted_gap (accepted_gap_id, project, gap_kind, title, reason, reopen_when, state)
			  VALUES ('AG-1', 'crew', 'data_boundary', 't', 'r', 'w', 'accepted')`)
	}
	wantErr(t, gap(crew), "thread:crew-1 does not write a gap_accepted event")
	ok(t, gap("owner"))
	ok(t, dw.run(t, store.Event{Stream: "ratchet:R-1", Type: "ratchet_registered", Actor: crew, Body: map[string]any{"ratchet_id": "R-1"}},
		`INSERT INTO agentrt.ratchet (ratchet_id, name, location, guards, strength, strength_class, state)
		  VALUES ('R-1', 'n', 'l', 'g', 's', 'blocking', 'active')`))
	// Nothing is deleted or truncated, and nothing appended is changed: the
	// runtime role has no such privilege; the owner role meets the triggers
	// on every row there is (a table it empties stays as it was).
	owner := dw.db.Connect(t, dw.db.Owner)
	column := map[string]string{"debt_text": "body", "debt_evidence": "quote", "debt_evidence_check": "result",
		"debt_part_landing": "commit_sha", "debt_acceptance_run": "result"}
	count := func(tbl string) (n int) {
		ok(t, owner.QueryRow(ctx, `SELECT count(*) FROM agentrt.`+tbl).Scan(&n))
		return n
	}
	for _, tbl := range append(slices.Clone(debtStateTables), debtAppendTables...) {
		before := count(tbl)
		for _, conn := range []*pgx.Conn{dw.rt, owner} {
			_, delErr := conn.Exec(ctx, `DELETE FROM agentrt.`+tbl)
			if _, err := conn.Exec(ctx, `TRUNCATE agentrt.`+tbl+` CASCADE`); err == nil || (conn == dw.rt && delErr == nil) {
				t.Errorf("%s: deleted or truncated (%v)", tbl, delErr)
			}
			if c, ok := column[tbl]; ok && (conn == dw.rt || before > 0) {
				if _, err := conn.Exec(ctx, `UPDATE agentrt.`+tbl+` SET `+c+` = `+c); err == nil {
					t.Errorf("%s: an appended row changed", tbl)
				}
			}
		}
		if after := count(tbl); after != before {
			t.Errorf("%s: %d rows, then %d", tbl, before, after)
		}
	}
	if _, err := store.VerifyStream(ctx, dw.rt, store.DebtStream(A)); err != nil {
		t.Fatal(err)
	}
}

// A link that would close a cycle of the waiting graph is refused, whatever
// kinds the cycle mixes (blocks and part_of together, §5.5); links that do
// not wait (related, follows_from) are not in it; an ended link no longer
// counts; the database refuses a cycle written past the store; and two
// halves of a cycle written at once are not both kept.
func TestDebtCyclesAreRefused(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	A, B, C := dw.mustFile(t, "stg", "a"), dw.mustFile(t, "stg", "b"), dw.mustFile(t, "stg", "c")
	link := func(kind, from, to, needs string) (map[string]any, error) {
		return store.LinkDebts(ctx, dw.rt, dw.crew, dw.key(), store.DebtLink{Kind: kind, From: from, To: to, Needs: needs})
	}
	cycle := func(err error, path ...string) {
		t.Helper()
		var ce *store.CycleError
		if !errors.As(err, &ce) || !errors.Is(err, store.ErrCycle) || fmt.Sprint(ce.Path) != fmt.Sprint(path) {
			t.Fatalf("err = %v, want the cycle %v", err, path)
		}
	}
	first, err := link("blocks", A, B, "closed")
	ok(t, err)
	_, err = link("blocks", B, A, "landed")
	cycle(err, A, B, A)
	_, err = link("part_of", B, A, "")
	cycle(err, A, B, A)
	_, err = link("blocks", A, B, "closed")
	wantIs(t, err, store.ErrState, "is linked already")
	_, err = link("part_of", B, C, "")
	ok(t, err)
	_, err = link("blocks", C, A, "closed")
	cycle(err, A, C, B, A)
	for _, kind := range []string{"related", "follows_from"} {
		_, err = link(kind, C, A, "")
		ok(t, err)
	}
	_, err = link("blocks", A, A, "closed")
	wantIs(t, err, store.ErrInvalid, "joins two debts")
	// Ended, A blocks B no longer counts: B waits on nothing, A may wait on B.
	_, err = store.EndDebtEdge(ctx, dw.rt, dw.crew, dw.key(), edgeID(t, first))
	ok(t, err)
	_, err = link("blocks", B, A, "landed")
	ok(t, err)
	// Past the store: A waits on B (B blocks A), so A blocks B closes a cycle.
	err = dw.run(t, de(A, "debt_edge_added", dw.crew.Actor, map[string]any{"debt_edge_id": 900010, "edge_kind": "blocks",
		"from_debt_id": A, "to_debt_id": B, "needs": "closed"}), fmt.Sprintf(`INSERT INTO agentrt.debt_edge (debt_edge_id, edge_kind, from_debt_id,
		  to_debt_id, needs, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (900010, 'blocks', '%s', '%s', 'closed', '%s')`, A, B, dw.crew.Actor))
	wantErr(t, err, "closes a cycle")

	// Two halves of one cycle at once: exactly one is kept.
	D, E := dw.mustFile(t, "stg", "d"), dw.mustFile(t, "stg", "e")
	pool := dw.db.Pool(t, dw.db.Runtime)
	errs := make([]error, 2)
	var start, wg sync.WaitGroup
	start.Add(1)
	for i, pair := range [][2]string{{D, E}, {E, D}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			_, errs[i] = store.LinkDebts(ctx, pool, wr(fmt.Sprintf("thread:half-%d", i)), "half",
				store.DebtLink{Kind: "blocks", From: pair[0], To: pair[1], Needs: "closed"})
		}()
	}
	start.Done()
	wg.Wait()
	kept := 0
	for _, err := range errs {
		switch {
		case err == nil:
			kept++
		case !errors.Is(err, store.ErrCycle):
			t.Fatalf("a half failed otherwise: %v", err)
		}
	}
	if kept != 1 {
		t.Fatalf("%d halves of the cycle were kept (%v), want 1", kept, errs)
	}
}

// edgeID is a link's id as an answer has it (an answer given again is read
// back from its stored form).
func edgeID(t *testing.T, out map[string]any) int64 {
	t.Helper()
	var n int64
	_, err := fmt.Sscan(fmt.Sprint(out["debt_edge_id"]), &n)
	ok(t, err)
	return n
}
