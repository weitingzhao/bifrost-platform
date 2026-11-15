package actionrecord_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// Origin: the Owner's redefinition of the card (2026-11-15, ~10:30): every
// action that is not a change of law is done by its line and recorded, for his
// review by seat, day and law. These tests keep the record's own rules: it
// says who, what, why, on which law and how it ended; no regret ready means no
// action done; it is append only; it reads back by seat and law.

func good() actionrecord.Record {
	return actionrecord.Record{Seat: "local_a", Action: "rollout_restart_deployment", Target: "cicd/deploy/x",
		Reason: "certificate reload", Regret: "restart again", RegretState: actionrecord.RegretReady,
		Law: "CLAUDE.md §3 B-tier", Outcome: actionrecord.OutcomeDone, CheckResult: "pod ready", NotVerified: "none"}
}

func TestNormalizeRefusesARecordThatSaysTooLittle(t *testing.T) {
	cases := map[string]func(*actionrecord.Record){
		"no seat":                   func(r *actionrecord.Record) { r.Seat = "" },
		"no reason":                 func(r *actionrecord.Record) { r.Reason = "  " },
		"no law":                    func(r *actionrecord.Record) { r.Law = "" },
		"bad action":                func(r *actionrecord.Record) { r.Action = "Not A Name" },
		"bad regret state":          func(r *actionrecord.Record) { r.RegretState = "maybe" },
		"bad outcome":               func(r *actionrecord.Record) { r.Outcome = "ok" },
		"ready regret says nothing": func(r *actionrecord.Record) { r.Regret = "" },
		"done without regret ready": func(r *actionrecord.Record) { r.RegretState = actionrecord.RegretNotReady },
		"bad hash":                  func(r *actionrecord.Record) { r.ParamsHash = "zz" },
	}
	for name, mutate := range cases {
		r := good()
		mutate(&r)
		if _, err := actionrecord.Normalize(r, nil); !errors.Is(err, actionrecord.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Positive controls: a refusal with no regret ready is a valid record; so is the good one.
	r := good()
	r.Outcome, r.RegretState = actionrecord.OutcomeRefused, actionrecord.RegretNotReady
	if _, err := actionrecord.Normalize(r, nil); err != nil {
		t.Errorf("refused with no regret: %v", err)
	}
	if _, err := actionrecord.Normalize(good(), nil); err != nil {
		t.Errorf("good: %v", err)
	}
}

func TestNormalizeRedactsAndFlattensFreeText(t *testing.T) {
	r := good()
	r.Reason = "line one\nline two secret=hunter2"
	got, err := actionrecord.Normalize(r, func(s string) string { return strings.ReplaceAll(s, "hunter2", "[redacted]") })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Reason, "hunter2") || strings.Contains(got.Reason, "\n") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestRecordsAreAppendOnlyAndReadBackBySeatAndLaw(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files); err != nil {
		t.Fatal(err)
	}
	runtime := db.Pool(t, db.Runtime)
	a := good()
	b := good()
	b.Seat, b.Law = "local_b", "OWNER_LAW row 7"
	id1, err := actionrecord.Insert(ctx, runtime, a, nil)
	if err != nil || id1 == 0 {
		t.Fatalf("insert a: %d %v", id1, err)
	}
	if _, err = actionrecord.Insert(ctx, runtime, b, nil); err != nil {
		t.Fatal(err)
	}
	// A correction names the record it corrects.
	fix := a
	fix.CorrectionOf, fix.CheckResult = id1, "pod was not ready after all"
	if _, err = actionrecord.Insert(ctx, runtime, fix, nil); err != nil {
		t.Fatal(err)
	}
	bySeat, more, err := actionrecord.List(ctx, runtime, actionrecord.Filter{Seat: "local_a"})
	if err != nil || more || len(bySeat) != 2 || bySeat[0].CorrectionOf != id1 {
		t.Fatalf("by seat: %d more=%v err=%v %+v", len(bySeat), more, err, bySeat)
	}
	byLaw, _, err := actionrecord.List(ctx, runtime, actionrecord.Filter{Law: "OWNER_LAW row 7"})
	if err != nil || len(byLaw) != 1 || byLaw[0].Seat != "local_b" {
		t.Fatalf("by law: %v %+v", err, byLaw)
	}
	page, more, err := actionrecord.List(ctx, runtime, actionrecord.Filter{Limit: 2})
	if err != nil || !more || len(page) != 2 {
		t.Fatalf("page: %d more=%v err=%v", len(page), more, err)
	}
	since, _, err := actionrecord.List(ctx, runtime, actionrecord.Filter{Since: time.Now().Add(time.Hour)})
	if err != nil || len(since) != 0 {
		t.Fatalf("since the future: %v %d", err, len(since))
	}
	// Append only: the runtime role cannot change or remove a record.
	for _, stmt := range []string{
		`UPDATE agentrt.action_record SET reason = 'x'`,
		`DELETE FROM agentrt.action_record`,
		`TRUNCATE agentrt.action_record`,
	} {
		if _, err = conn.Exec(ctx, stmt); err == nil {
			t.Errorf("%s was allowed", stmt)
		}
	}
	// The table itself refuses a done action with no regret ready.
	if _, err = conn.Exec(ctx, `INSERT INTO agentrt.action_record (seat, action, reason, regret, regret_state, law, outcome)
	  VALUES ('s','a','r','','not_ready','l','done')`); err == nil {
		t.Error("the table took a done action without a regret ready")
	}
}
