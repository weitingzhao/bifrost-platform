package store_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Round 3 of the review of the debt guards (review 631c): the reviewer's
// probes, their attacks kept, with the allowed counterparts.

// importAwaiting imports debt awaiting the Owner, as the import would write
// one of the ledger's lines: with a part that signs a rule (part 0: none),
// the sign-off summary, ratchet and follow-up texts, and an imported run of
// result ("" for none); ids from base.
func importAwaiting(t *testing.T, dw *debtWorld, debt, result string, part, base int64) {
	t.Helper()
	importAwaitingOf(t, dw, debt, result, part, base, []string{"signoff_summary", "ratchet_text", "follow_up_text"})
}

func importAwaitingOf(t *testing.T, dw *debtWorld, debt, result string, part, base int64, kinds []string) {
	t.Helper()
	body := map[string]any{"source": "import", "project": "crew", "priority": "P3", "state": "awaiting_signoff", "flow": "manual"}
	stmts := []string{insertDebtSQL(debt, "awaiting_signoff", "controller")}
	var texts []any
	for i, kind := range kinds {
		id := base + int64(i) + 1
		texts = append(texts, id)
		stmts = append(stmts, debtTextSQL(id, debt, kind, "controller"))
	}
	if len(texts) > 0 {
		body["debt_text_ids"] = texts
	}
	if part != 0 {
		body["debt_part_ids"] = []any{part}
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO agentrt.debt_part (debt_part_id, debt_id, repo, ordinal, apply_gate) OVERRIDING SYSTEM VALUE
		  VALUES (%d, '%s', 'repo-a', 1, 'rule_sign')`, part, debt))
	}
	ok(t, dw.run(t, de(debt, "debt_filed", "controller", body), stmts...))
	if result != "" {
		run := base + 9
		ok(t, dw.run(t, de(debt, "debt_acceptance_ran", "controller", map[string]any{"debt_acceptance_run_id": run, "runner": "imported",
			"result": result}), fmt.Sprintf(`INSERT INTO agentrt.debt_acceptance_run (debt_acceptance_run_id, debt_id, runner, source_sha256, result,
			  ran_at, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (%d, '%s', 'imported', repeat('0', 64), '%s', now(), 'controller')`,
			run, debt, result)))
	}
}

// R3-M1: a debt imported awaiting the Owner is signed on the ledger's word,
// but on that word: its latest run is the imported pass, and it keeps the
// summary, ratchet and follow-up lines the ledger gave.
func TestReviewR3ImportedSignoffRequiresImportedPass(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, result string
		kinds        []string
		refusal      string
	}{
		{"no run", "", nil, "not the ledger's imported pass"},
		{"an imported fail", "fail", nil, "not the ledger's imported pass"},
		{"an imported partial", "partial", nil, "not the ledger's imported pass"},
		{"a pass without its summary", "pass", []string{"ratchet_text", "follow_up_text"}, "no sign-off summary"},
		{"a pass without its ratchet", "pass", []string{"signoff_summary", "follow_up_text"}, "names no ratchet"},
		{"a pass without its follow-up", "pass", []string{"signoff_summary", "ratchet_text"}, "names no follow-up"},
		{"a pass with its lines", "pass", nil, ""},
		{"a pass saying why none", "pass", []string{"signoff_summary", "no_ratchet_reason", "no_follow_up_reason"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dw := newDebtWorld(t, true)
			kinds := c.kinds
			if kinds == nil {
				kinds = []string{"signoff_summary", "ratchet_text", "follow_up_text"}
			}
			importAwaitingOf(t, dw, "TDS-980", c.result, 0, 980000, kinds)
			err := (&closing{dw: dw, t: t, D: "TDS-980"}).signOff()
			if c.refusal == "" {
				ok(t, err)
			} else {
				wantErr(t, err, c.refusal)
			}
		})
	}
}

// R3-S1: linking work leaves a debt awaiting the Owner where it is, so the
// ledger's word still stands for it; a real move away (rejected) is what
// loses it (TestDebtImportedSignoffOnlyWhileNeverMoved).
func TestReviewR3ImportedWorkLinkDoesNotLeaveAwaiting(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	dw.mustFile(t, "stg", "registers the crew")
	const debt = "TDS-980"
	importAwaiting(t, dw, debt, "pass", 0, 980000)
	ev := de(debt, "debt_work_linked", dw.crew.Actor, map[string]any{"debt_work_id": 980005})
	ev.MissionID = "M-1"
	ok(t, dw.run(t, ev, `INSERT INTO agentrt.debt_work (debt_work_id, debt_id, mission_id, started_at)
	  OVERRIDING SYSTEM VALUE VALUES (980005, 'TDS-980', 'M-1', now())`))
	v, err := store.DebtState(ctx, dw.rt, debt)
	ok(t, err)
	if v.State != "awaiting_signoff" {
		t.Fatalf("linking work moved it to %s", v.State)
	}
	ok(t, (&closing{dw: dw, t: t, D: debt}).signOff())
}

// R3-M2: a landing is of its part's repository: once a part has one, its
// repository stays (another repository is another part, which lands on its
// own); before any landing the repository may still change, and the part
// then lands and closes in the new one.
func TestReviewR3ChangedPartRepoNeedsNewLanding(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	repo := func(c *closing, to string) error {
		return dw.run(t, de(c.D, "debt_part_set", dw.crew.Actor, map[string]any{"debt_part_ids": []any{c.part}}),
			fmt.Sprintf(`UPDATE agentrt.debt_part SET repo = '%s', version = version + 1 WHERE debt_part_id = %d`, to, c.part))
	}
	c := dw.closingDebt(t, 970000)
	c.ready()
	c.signsByRule()
	ok(t, c.toOwner())
	wantErr(t, repo(c, "repo-b"), "another repository is another part")
	// The allowed one: the repository changes before the part lands.
	d := dw.closingDebt(t, 990000)
	ok(t, repo(d, "repo-b"))
	d.land("0123456789abcdef0123456789abcdef01234567", "2 days")
	ok(t, d.watch())
	d.review()
	source, checked := d.id(), d.id()
	withRepoB := func(sql string) string {
		return strings.Replace(sql, `{"repo-a": "0123abc"}`, `{"repo-b": "0123abc"}`, 1)
	}
	ok(t, store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		var err error
		d.proof, err = store.PutArtifact(ctx, tx, dw.crew.Actor, "report", "text/markdown", []byte("repo-b looked"))
		return err
	}))
	ok(t, dw.run(t, ranEvent(d.D, source, "unknown", dw.crew.Actor),
		withRepoB(debtRunSQL(source, d.D, "manual", "unknown", dw.crew.Actor, "", d.proof, 0))))
	ok(t, dw.run(t, ranEvent(d.D, checked, "pass", crew2),
		withRepoB(debtRunSQL(checked, d.D, "manual", "pass", dw.crew.Actor, crew2, d.proof, source))))
	d.texts()
	ok(t, d.autoClose())
}

// R3-S2: a record from before a reopening that nobody checked, its real
// time and evidence kept, is refused by the reopening's own condition.
func TestReviewR3UnusedRunBeforeReopenIsRejected(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 970000)
	c.land("0123456789abcdef0123456789abcdef01234567", "2 days")
	ok(t, c.watch())
	c.review()
	unused := c.record()
	unusedProof := c.proof
	ok(t, c.check(c.record()))
	c.texts()
	ok(t, c.autoClose())
	x := c.id()
	ok(t, dw.run(t, de(c.D, "debt_reopened", dw.crew.Actor, map[string]any{"debt_text_ids": []any{x}, "from": "closed", "to": "doing"}),
		debtTextSQL(x, c.D, "note", dw.crew.Actor), debtMoveSQL(c.D, "doing", "")))
	c.proof = unusedProof
	wantErr(t, c.check(unused), "ran before the last landing or reopening")
}

// R3-S2: two debts may share one report as evidence, each with its own
// record and check; a record of one debt never stands for the other.
func TestReviewR3TwoDebtsCanUseTheSameArtifact(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	a, b := dw.closingDebt(t, 970000), dw.closingDebt(t, 990000)
	var proof string
	ok(t, store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		var err error
		proof, err = store.PutArtifact(ctx, tx, dw.crew.Actor, "report", "text/markdown", []byte("shared evidence for two debts"))
		return err
	}))
	var firstSource int64
	for i, c := range []*closing{a, b} {
		c.land("0123456789abcdef0123456789abcdef01234567", "2 days")
		ok(t, c.watch())
		c.review()
		c.texts()
		c.proof = proof
		if i == 1 {
			wantErr(t, c.check(firstSource), "same evidence, spec, commits and run time")
		}
		source := c.id()
		ok(t, dw.run(t, ranEvent(c.D, source, "unknown", dw.crew.Actor),
			debtRunSQL(source, c.D, "manual", "unknown", dw.crew.Actor, "", proof, 0)))
		ok(t, c.check(source))
		ok(t, c.autoClose())
		if i == 0 {
			firstSource = source
		}
	}
}

// R3-S2: only a checker's manual result names a record it checks: an Owner's
// or an imported run naming one is refused by the table's check, and taken
// without.
func TestReviewR3OwnerAndImportedRunsHaveNoChecksRun(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 970000)
	source := c.record()
	for _, runner := range []string{"owner", "imported"} {
		for _, hasSource := range []bool{false, true} {
			id := c.id()
			ev := de(c.D, "debt_acceptance_ran", "controller", map[string]any{"debt_acceptance_run_id": id, "runner": runner, "result": "pass"})
			ref := int64(0)
			if hasSource {
				ref = source
			}
			sql := debtRunSQL(id, c.D, runner, "pass", "controller", "", c.proof, ref)
			if runner == "imported" {
				sql = strings.Replace(sql, "spec_sha256, result", "source_sha256, result", 1)
			}
			if err := dw.run(t, ev, sql); hasSource {
				wantErr(t, err, "check constraint")
			} else {
				ok(t, err)
			}
		}
	}
}
