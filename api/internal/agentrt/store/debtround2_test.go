package store_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Round 2 of the review of the debt guards (review 631b): the three probes
// the reviewer wrote (R2-M1 to R2-M3, their attacks kept as they were), each
// with its allowed counterpart, on one fixture builder.

const crew2 = "thread:crew-2"

// closing is a debt being closed, past the store: its writers and ids.
type closing struct {
	dw    *debtWorld
	t     *testing.T
	D     string
	part  int64
	proof string
	next  int64
}

func (c *closing) id() int64 { c.next++; return c.next }

// closingDebt files a debt with a spec and a part and starts it with live
// work (open to doing), the second crew writer registered.
func (dw *debtWorld) closingDebt(t *testing.T, base int64) *closing {
	t.Helper()
	c := &closing{dw: dw, t: t, D: dw.fileWithSpec(t, "closes"), next: base}
	dw.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:crew-2', 'thread', 'crew 2')
	  ON CONFLICT DO NOTHING`)
	ok(t, dw.rt.QueryRow(ctx, `SELECT debt_part_id FROM agentrt.debt_part WHERE debt_id = $1`, c.D).Scan(&c.part))
	w := c.id()
	work := de(c.D, "debt_work_linked", dw.crew.Actor, map[string]any{"debt_work_id": w, "from": "open", "to": "doing"})
	work.MissionID = "M-1"
	ok(t, dw.run(t, work, fmt.Sprintf(`INSERT INTO agentrt.debt_work (debt_work_id, debt_id, mission_id, started_at)
	  OVERRIDING SYSTEM VALUE VALUES (%d, '%s', 'M-1', now())`, w, c.D), debtMoveSQL(c.D, "doing", "")))
	return c
}

// land records a landing the runtime verified, deployed ago.
func (c *closing) land(commit, ago string) {
	l := c.id()
	ok(c.t, c.dw.run(c.t, de(c.D, "debt_part_landed", "controller", map[string]any{"debt_part_id": c.part, "debt_part_landing_id": l,
		"commit": commit, "via": "runtime_landing"}), fmt.Sprintf(`INSERT INTO agentrt.debt_part_landing (debt_part_landing_id, debt_part_id,
		  commit_sha, via, deployed_env, deployed_at, deploy_read, created_by_actor_id) OVERRIDING SYSTEM VALUE
		  VALUES (%d, %d, '%s', 'runtime_landing', 'stg', now() - interval '%s', 'observed', 'controller')`, l, c.part, commit, ago)))
}

// watch moves doing to watching, the watch already over.
func (c *closing) watch() error {
	w := c.id()
	return c.dw.run(c.t, de(c.D, "debt_state_changed", c.dw.crew.Actor, map[string]any{"from": "doing", "to": "watching",
		"debt_text_ids": []any{w}}), debtTextSQL(w, c.D, "watch", c.dw.crew.Actor), fmt.Sprintf(`UPDATE agentrt.debt SET state = 'watching',
		  watch_until = now() - interval '1 hour', watch_text_id = %d, state_since = clock_timestamp(), version = version + 1
		  WHERE debt_id = '%s'`, w, c.D))
}

func (c *closing) review() {
	ok(c.t, c.dw.run(c.t, de(c.D, "debt_acceptance_reviewed", crew2, map[string]any{"acceptance_sha256": c.dw.specSHA(c.t, c.D)}),
		fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_reviewed_sha256 = agentrt.debt_spec_sha(acceptance_json),
		  acceptance_reviewed_by = '%s', version = version + 1 WHERE debt_id = '%s'`, crew2, c.D)))
}

// record is the author's manual record (unknown) on a fresh artifact; check
// is the second writer's pass over a record.
func (c *closing) record() int64 {
	ok(c.t, store.InTx(ctx, c.dw.rt, func(tx pgx.Tx) error {
		var err error
		c.proof, err = store.PutArtifact(ctx, tx, c.dw.crew.Actor, "report", "text/markdown", []byte(fmt.Sprintf("looked %d", c.next)))
		return err
	}))
	r := c.id()
	ok(c.t, c.dw.run(c.t, ranEvent(c.D, r, "unknown", c.dw.crew.Actor),
		debtRunSQL(r, c.D, "manual", "unknown", c.dw.crew.Actor, "", c.proof, 0)))
	return r
}

func (c *closing) check(of int64) error {
	r := c.id()
	return c.dw.run(c.t, ranEvent(c.D, r, "pass", crew2), debtRunSQL(r, c.D, "manual", "pass", c.dw.crew.Actor, crew2, c.proof, of))
}

func ranEvent(debt string, id int64, result, actor string) store.Event {
	return de(debt, "debt_acceptance_ran", actor, map[string]any{"debt_acceptance_run_id": id, "runner": "manual", "result": result})
}

func (c *closing) texts() {
	for _, kind := range []string{"signoff_summary", "no_ratchet_reason", "no_follow_up_reason"} {
		x := c.id()
		ok(c.t, c.dw.run(c.t, de(c.D, "debt_text_added", c.dw.crew.Actor, map[string]any{"debt_text_id": x, "text_kind": kind,
			"sha256": "3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d"}), debtTextSQL(x, c.D, kind, c.dw.crew.Actor)))
	}
}

// ready brings a started debt to watching with every close condition met:
// a landing deployed two days ago, the watch over, the spec reviewed, a
// checked pass, the three texts.
func (c *closing) ready() {
	c.land("0123456789abcdef0123456789abcdef01234567", "2 days")
	ok(c.t, c.watch())
	c.review()
	ok(c.t, c.check(c.record()))
	c.texts()
}

func (c *closing) autoClose() error {
	return c.dw.run(c.t, de(c.D, "debt_auto_closed", "controller", map[string]any{"from": "watching", "to": "closed"}), fmt.Sprintf(`UPDATE agentrt.debt SET state = 'closed',
	  conclusion = 'auto_closed', watch_until = NULL, watch_text_id = NULL, state_since = clock_timestamp(), version = version + 1
	  WHERE debt_id = '%s'`, c.D))
}

func (c *closing) toOwner() error {
	return c.dw.run(c.t, de(c.D, "debt_state_changed", c.dw.crew.Actor, map[string]any{"from": "watching", "to": "awaiting_signoff"}),
		fmt.Sprintf(`UPDATE agentrt.debt SET state = 'awaiting_signoff', watch_until = NULL, watch_text_id = NULL,
		  state_since = clock_timestamp(), version = version + 1 WHERE debt_id = '%s'`, c.D))
}

func (c *closing) signOff() error {
	return c.dw.run(c.t, de(c.D, "debt_signed_off", "owner", map[string]any{"from": "awaiting_signoff", "to": "closed"}),
		debtMoveSQL(c.D, "closed", "signed_off"))
}

// signsByRule marks the part as one that signs a rule: the Owner signs the
// debt off (T12).
func (c *closing) signsByRule() {
	ok(c.t, c.dw.run(c.t, de(c.D, "debt_part_set", c.dw.crew.Actor, map[string]any{"debt_part_ids": []any{c.part}}),
		fmt.Sprintf(`UPDATE agentrt.debt_part SET apply_gate = 'rule_sign', version = version + 1 WHERE debt_part_id = %d`, c.part)))
}

// R2-M1: a deploy after the watch began restarts it: closing on its own,
// and awaiting the Owner, wait until the last deploy is 24 hours old, a
// fresh pass of the new deploy notwithstanding.
func TestReviewNewDeployMustRestartWatch(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 910000)
	c.ready()
	c.land("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "0 seconds")
	ok(t, c.check(c.record()))
	wantErr(t, c.autoClose(), "its last deploy has not been watched for 24 hours")
	c.signsByRule()
	wantErr(t, c.toOwner(), "its last deploy has not been watched for 24 hours")
	// The allowed one: a debt whose last deploy is 24 hours old closes.
	d := dw.closingDebt(t, 920000)
	d.ready()
	ok(t, d.autoClose())
}

// R2-M3: after a reopening the old runs no longer count (§6.2): a checker's
// pass names the one record it checks, keeps that record's run time, checks
// it once, and only a record made after the last landing and reopening.
func TestReviewReopenMustNotReuseOldManualRun(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 910000)
	c.land("0123456789abcdef0123456789abcdef01234567", "2 days")
	ok(t, c.watch())
	c.review()
	old := c.record()
	ok(t, c.check(old))
	wantErr(t, c.check(old), "debt_acceptance_run_checks_once")
	c.texts()
	ok(t, c.autoClose())
	x := c.id()
	ok(t, dw.run(t, de(c.D, "debt_reopened", dw.crew.Actor, map[string]any{"debt_text_ids": []any{x}, "from": "closed", "to": "doing"}),
		debtTextSQL(x, c.D, "note", dw.crew.Actor), debtMoveSQL(c.D, "doing", "")))
	ok(t, c.watch())
	wantErr(t, c.autoClose(), "older than its last landing or reopening")
	// The reviewer's replay: the old record, artifact, spec and commits,
	// recorded with the time of now instead of the record's.
	stale := c.id()
	keep := fmt.Sprintf("COALESCE((SELECT ran_at FROM agentrt.debt_acceptance_run WHERE debt_acceptance_run_id = %d), now())", old)
	wantErr(t, dw.run(t, ranEvent(c.D, stale, "pass", crew2), strings.Replace(debtRunSQL(stale, c.D, "manual", "pass", dw.crew.Actor,
		crew2, c.proof, old), keep, "now()", 1)), "")
	wantErr(t, c.autoClose(), "older than its last landing or reopening")
	// The allowed one: a new record, checked, closes it again.
	ok(t, c.check(c.record()))
	ok(t, c.autoClose())
}

// R2-M3: a record made before a later landing is not checked any more, and a
// check keeps the run time of the record it checks.
func TestDebtCheckOfARecordBeforeTheLastLanding(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 910000)
	c.land("0123456789abcdef0123456789abcdef01234567", "2 days")
	early := c.record()
	c.land("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "2 days")
	wantErr(t, c.check(early), "ran before the last landing or reopening")
	fresh := c.record()
	moved := c.id()
	keep := fmt.Sprintf("COALESCE((SELECT ran_at FROM agentrt.debt_acceptance_run WHERE debt_acceptance_run_id = %d), now())", fresh)
	wantErr(t, dw.run(t, ranEvent(c.D, moved, "pass", crew2), strings.Replace(debtRunSQL(moved, c.D, "manual", "pass", dw.crew.Actor,
		crew2, c.proof, fresh), keep, "(SELECT ran_at - interval '1 second' FROM agentrt.debt_acceptance_run WHERE debt_acceptance_run_id = "+
		fmt.Sprint(fresh)+")", 1)), "same evidence, spec, commits and run time")
	ok(t, c.check(fresh))
}

// R2-M2: the Owner's sign-off rechecks every close condition: a spec changed
// while the debt awaited him leaves nothing reviewed or passing, and he
// cannot sign it; with the conditions met he can.
func TestReviewSignoffMustRecheckChangedSpec(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 910000)
	c.ready()
	c.signsByRule()
	ok(t, c.toOwner())
	const replacement = `[ {"kind":"manual", "say":"check a different requirement", "expect":"fine"} ]`
	var sum string
	ok(t, dw.rt.QueryRow(ctx, `SELECT agentrt.debt_spec_sha($1::jsonb)`, replacement).Scan(&sum))
	ok(t, dw.run(t, de(c.D, "debt_acceptance_set", dw.crew.Actor, map[string]any{"acceptance_sha256": sum}),
		fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_json = '%s', acceptance_by = '%s', acceptance_reviewed_sha256 = NULL,
		  acceptance_reviewed_by = NULL, version = version + 1 WHERE debt_id = '%s'`, replacement, dw.crew.Actor, c.D)))
	wantErr(t, c.signOff(), "its acceptance spec was not reviewed")
	d := dw.closingDebt(t, 920000)
	d.ready()
	d.signsByRule()
	ok(t, d.toOwner())
	ok(t, d.signOff())
}

// R2-M2: the ledger's imported pass stands for the sign-off only while the
// debt has awaited the Owner since its import, as its history says: one
// rejected and brought back must meet every condition again.
func TestDebtImportedSignoffOnlyWhileNeverMoved(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	importAwaiting(t, dw, "TDS-951", "pass", 930001, 930010)
	importAwaiting(t, dw, "TDS-952", "pass", 930002, 930020)
	ok(t, (&closing{dw: dw, t: t, D: "TDS-951"}).signOff())
	c := &closing{dw: dw, t: t, D: "TDS-952", part: 930002, next: 930100}
	dw.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:crew-1', 'thread', 'crew 1'),
	  ('thread:crew-2', 'thread', 'crew 2')`)
	r := c.id()
	ok(t, dw.run(t, de(c.D, "debt_signoff_rejected", "owner", map[string]any{"debt_text_ids": []any{r}, "from": "awaiting_signoff", "to": "doing"}),
		debtTextSQL(r, c.D, "reject_reason", "owner"), debtMoveSQL(c.D, "doing", "")))
	spec := func(say string) error {
		return dw.run(t, de(c.D, "debt_acceptance_set", dw.crew.Actor, map[string]any{"acceptance_sha256": strings.Repeat("0", 64)}),
			fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_json = '[{"kind":"manual","say":"%s","expect":"fine"}]', acceptance_by = '%s',
			  acceptance_reviewed_sha256 = NULL, acceptance_reviewed_by = NULL, version = version + 1 WHERE debt_id = '%s'`, say, dw.crew.Actor, c.D))
	}
	ok(t, spec("look"))
	c.ready()
	ok(t, c.toOwner())
	ok(t, spec("look again"))
	wantErr(t, c.signOff(), "its acceptance spec was not reviewed")
}
