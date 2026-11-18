package evidence

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor" // the controller seed and its vocabulary, in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

func verdictRun(t *testing.T, name, result, scope string, audit bool, at time.Time) EvidenceRow {
	t.Helper()
	h := func(c string) string { return strings.Repeat(c, 40) }
	reason := "Succeeded"
	if result == "Failed" {
		reason = "Failed"
	}
	e, is, err := RunEvidence(Run{Kind: "ci", Repo: "bifrost-platform", Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform",
		Result: result, Reason: reason, Scope: scope, Audit: audit, CompletedAt: at,
		Inputs: []RepoAt{{Repo: "bifrost-platform", Commit: h("a"), Tree: h("c")}}})
	if err != nil || !is {
		t.Fatalf("run %s: %v %v", name, is, err)
	}
	return e
}

func seededLedger(t *testing.T) eventlog.DB {
	t.Helper()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(context.Background(), db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	return db.Pool(t, db.Runtime)
}

type verdictEvent struct {
	body map[string]any
	txid int64
}

func verdictEvents(t *testing.T, db eventlog.DB) []verdictEvent {
	t.Helper()
	var out []verdictEvent
	err := eventlog.InTx(context.Background(), db, func(tx pgx.Tx) error {
		out = nil
		rows, err := tx.Query(context.Background(), `SELECT body_json::text, txid FROM agentrt.event WHERE event_type = $1 ORDER BY event_id`, VerdictChangedType)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			var ev verdictEvent
			if err = rows.Scan(&raw, &ev.txid); err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(raw), &ev.body); err != nil {
				return err
			}
			out = append(out, ev)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// 2026-11-16: bus step 3 — ci.verdict.changed announced in the evidence write's transaction.
// One event per change of a key's verdict (none to green, green to red when
// the daily full CI invalidates the pass), none for a row seen again or a row
// that leaves the verdict as it was; every body passes CheckBody; the event
// was written by the transaction that wrote its row.
func TestVerdictChangedOncePerChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := seededLedger(t)
	at := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	pass := verdictRun(t, "ci-pass", "Succeeded", "full", false, at)
	steps := []struct {
		row  EvidenceRow
		want int
	}{
		{pass, 1}, // none -> green
		{pass, 1}, // the same row again: nothing written
		{verdictRun(t, "ci-part", "Succeeded", "incremental", false, at.Add(time.Minute)), 1}, // still green
		{verdictRun(t, "ci-audit", "Failed", "full", true, at.Add(2*time.Minute)), 2},         // green -> red
	}
	for i, s := range steps {
		if _, err := RecordEvidence(ctx, db, []EvidenceRow{s.row}); err != nil {
			t.Fatalf("step %d: %v", i+1, err)
		}
		if got := len(verdictEvents(t, db)); got != s.want {
			t.Fatalf("step %d: %d events, want %d", i+1, got, s.want)
		}
	}
	events := verdictEvents(t, db)
	for i, want := range [][2]string{{EvidenceGreen, EvidenceNone}, {EvidenceRed, EvidenceGreen}} {
		b := events[i].body
		if b["verdict"] != want[0] || b["previous"] != want[1] || b["change"] != want[0] || b["kind"] != VerdictKindCI {
			t.Errorf("event %d: %v", i+1, b)
		}
		if err := eventlog.CheckBody(VerdictChangedType, b); err != nil {
			t.Errorf("event %d: %v", i+1, err)
		}
	}
	// Same transaction: each event's txid is the xid that wrote the row it
	// announces (a fresh database is in epoch 0).
	for i, run := range []string{"ci-pass", "ci-audit"} {
		var rowXID int64
		err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT max(xmin::text::bigint) FROM agentrt.evidence WHERE run_name = $1`, run).Scan(&rowXID)
		})
		if err != nil || rowXID != events[i].txid {
			t.Errorf("event %d: txid %d, its row's %d (%v)", i+1, events[i].txid, rowXID, err)
		}
	}
	if _, err := eventlog.VerifyStream(ctx, db, VerdictStream("bifrost-platform", strings.Repeat("c", 40))); err != nil {
		t.Fatal(err)
	}
}

// 2026-11-16: bus step 3 — ci.verdict.changed announced in the evidence write's transaction.
// The announcement lives in its caller's transaction: rolled back, neither
// the row nor the event is there. The positive control commits the same
// writes and finds both.
func TestVerdictChangedRollsBackWithTheRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := seededLedger(t)
	e := verdictRun(t, "ci-rb", "Succeeded", "full", false, time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	write := func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.evidence (repo, unit, task, input_key, result, scope, inputs, statement, commit_sha, run_name, run_uid)
		  VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11)`,
			e.Repo, e.Unit, e.Task, e.InputKey, e.Result, e.Scope, string(e.Inputs), string(e.Statement), e.CommitSHA, e.RunName, e.RunUID); err != nil {
			return err
		}
		return announceVerdict(ctx, tx, e, EvidenceNone)
	}
	errRollback := eventlog.Invalid("roll back")
	if err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := write(tx); err != nil {
			return err
		}
		return errRollback
	}); err != errRollback {
		t.Fatalf("rolled-back write: %v", err)
	}
	count := func() (rows, events int) {
		t.Helper()
		if err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.evidence WHERE run_name = 'ci-rb'`).Scan(&rows); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1`, VerdictChangedType).Scan(&events)
		}); err != nil {
			t.Fatal(err)
		}
		return
	}
	if r, n := count(); r != 0 || n != 0 {
		t.Fatalf("after the rollback: %d rows, %d events", r, n)
	}
	if err := eventlog.InTx(ctx, db, write); err != nil {
		t.Fatal(err)
	}
	if r, n := count(); r != 1 || n != 1 {
		t.Fatalf("positive control: %d rows, %d events", r, n)
	}
}

// 2026-11-16: bus step 3 — ci.verdict.changed announced in the evidence write's transaction.
// The type is the register's (publisher gate-db, key tree), its body
// requires the fixed fields the entry promises, the ref is the register's
// with its placeholders filled, and CheckBody refuses a body whose verdict
// did not change or whose subject is not made of its facts.
func TestVerdictChangedAgreesWithTheRegister(t *testing.T) {
	t.Parallel()
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	ch, ok := all["rocket"].Channels[VerdictChangedType]
	if !ok || ch.Publisher != "gate-db" || ch.Key != "tree" {
		t.Fatalf("registered %v, publisher %q, key %q", ok, ch.Publisher, ch.Key)
	}
	fields, ok := eventlog.BodyOf(VerdictChangedType)
	if !ok {
		t.Fatal("no declared body")
	}
	required := map[string]bool{}
	for _, f := range fields {
		required[f.Name] = !f.Optional
	}
	for _, n := range []string{"subject", "ref", "change", "key", ch.Key} {
		if !required[n] {
			t.Errorf("the body does not require %q", n)
		}
	}
	h := func(c string) string { return strings.Repeat(c, 40) }
	pattern := "^" + regexp.MustCompile(`\\\{[a-z]+\\\}`).ReplaceAllString(regexp.QuoteMeta(ch.Ref), `[A-Za-z0-9._-]+`) + "$"
	if ref := VerdictRef("bifrost-platform", h("a")); !regexp.MustCompile(pattern).MatchString(ref) {
		t.Errorf("ref %q is not the register's %q", ref, ch.Ref)
	}
	key := strings.Repeat("e", 64)
	good := VerdictBody("bifrost-platform", h("a"), h("c"), VerdictKindCI, EvidenceScopeFull, EvidenceUnitRepo, "bifrost-ci-platform", key, EvidenceGreen, EvidenceNone)
	if err := eventlog.CheckBody(VerdictChangedType, good); err != nil {
		t.Fatalf("positive control: %v", err)
	}
	// 2026-11-16, CI line: one kind ci, full against incremental in the
	// subject, because the envelope carries no data.
	if got, want := good["subject"], h("c")+"@"+h("a")+":ci/full"; got != want {
		t.Errorf("subject = %v, want %v", got, want)
	}
	if got := VerdictSubject(h("c"), h("a"), VerdictKindUnit, EvidenceScopeFull); got != h("c")+"@"+h("a")+":unit" {
		t.Errorf("a unit key's subject carries no scope: %v", got)
	}
	same := VerdictBody("bifrost-platform", h("a"), h("c"), VerdictKindCI, EvidenceScopeFull, EvidenceUnitRepo, "bifrost-ci-platform", key, EvidenceGreen, EvidenceGreen)
	if eventlog.CheckBody(VerdictChangedType, same) == nil {
		t.Error("a body whose verdict did not change is taken")
	}
	bad := VerdictBody("bifrost-platform", h("a"), h("c"), VerdictKindCI, EvidenceScopeFull, EvidenceUnitRepo, "bifrost-ci-platform", key, EvidenceGreen, EvidenceNone)
	bad["subject"] = h("c") + "@" + h("b") + ":ci"
	if eventlog.CheckBody(VerdictChangedType, bad) == nil {
		t.Error("a subject not made of its facts is taken")
	}
}
