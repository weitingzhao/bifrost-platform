//bifrost:owner plan

package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
)

// refused holds an error to the refusal of an unbound port.
func refused(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, store.ErrPortUnbound) {
		t.Fatalf("%s, unbound: %v; want ErrPortUnbound", what, err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the plan code reads the rules in force through a port, and a port nobody bound must refuse the proposal or the card, never answer "no rule in force" (which would send every change to the Owner's card, or ask every card of the Owner, with nothing saying why).
func TestUnboundRulePortsRefuse(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]*store.RulePorts{"nothing bound": nil, "an empty binding": {}} {
		r, err := store.PlanRuleViaForTest(ctx, p, nil)
		refused(t, name+": the plan-standing rule", err)
		if r != nil {
			t.Fatalf("%s: a rule with the refusal: %+v", name, r)
		}
		kinds, err := store.CardAutoKindsViaForTest(ctx, p, nil)
		refused(t, name+": the card-auto-answer rule", err)
		if kinds != nil {
			t.Fatalf("%s: kinds with the refusal: %v", name, kinds)
		}
	}
	// The positive control: a bound port is called and its answer returned.
	want := &store.PlanRule{ID: "plan-standing", Version: 7, DoneRules: true}
	bound := &store.RulePorts{
		PlanRuleInForce:      func(context.Context, pgx.Tx) (*store.PlanRule, error) { return want, nil },
		CardAutoKindsInForce: func(context.Context, pgx.Tx) ([]string, error) { return []string{"blocked"}, nil },
	}
	if r, err := store.PlanRuleViaForTest(ctx, bound, nil); err != nil || r != want {
		t.Fatalf("a bound port: %+v %v", r, err)
	}
	if kinds, err := store.CardAutoKindsViaForTest(ctx, bound, nil); err != nil || !reflect.DeepEqual(kinds, []string{"blocked"}) {
		t.Fatalf("a bound port: %v %v", kinds, err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the rules' side binds the two rule ports at the package's start (envelope.go); this holds that binding in place and that each port answers what the rule store itself answers, with a rule in force and with none.
func TestRulePortsAreBoundToTheRuleStore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	read := func() (plan *store.PlanRule, kinds []string, direct [2]*store.StoredRule) {
		t.Helper()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			if plan, err = store.PlanRuleInForceForTest(ctx, tx); err != nil {
				return err
			}
			if kinds, err = store.CardAutoKindsInForceForTest(ctx, tx); err != nil {
				return err
			}
			if direct[0], err = store.CurrentRule(ctx, tx, rules.KindPlan, time.Now()); err != nil {
				return err
			}
			direct[1], err = store.CurrentRule(ctx, tx, rules.KindCardAuto, time.Now())
			return err
		}))
		return plan, kinds, direct
	}
	// No rule in force: the ports say none, as the rule store does.
	if plan, kinds, direct := read(); plan != nil || kinds != nil || direct[0] != nil || direct[1] != nil {
		t.Fatalf("with no rule: %+v %v %+v", plan, kinds, direct)
	}
	putPlanStanding(t, w, true, false)
	_, err := w.rt.Exec(ctx, agentrttest.CardAutoRule)
	ok(t, err)
	plan, kinds, direct := read()
	if direct[0] == nil || direct[1] == nil || len(direct[1].CardAuto.Kinds) == 0 || !direct[0].Plan.DoneRules {
		t.Fatalf("the fixture rules are not in force: %+v", direct)
	}
	if plan == nil || *plan != (store.PlanRule{ID: direct[0].ID, Version: direct[0].Version, DoneRules: direct[0].Plan.DoneRules}) {
		t.Fatalf("the plan-standing port: %+v; the rule store: %+v", plan, direct[0])
	}
	if !reflect.DeepEqual(kinds, direct[1].CardAuto.Kinds) {
		t.Fatalf("the card-auto-answer port: %v; the rule store: %v", kinds, direct[1].CardAuto.Kinds)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the plan code checks external work's note and description and reads a plan's registered work through ports, and decides a recorder import through two more; unbound, each must refuse (the note or artifact is not stored, the plan view and the decision fail), never pass a record unchecked, show a plan with no external work, or decide a recorder import as a plan change.
func TestUnboundLineagePortsRefuse(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]*store.ExternalWorkPorts{"nothing bound": nil, "an empty binding": {}} {
		refused(t, name+": the note check", store.CheckExternalNoteViaForTest(p, map[string]any{}))
		refused(t, name+": the description check", store.CheckExternalDescriptionViaForTest(p, []byte(`{}`)))
		work, err := store.ExternalWorkSnapshotViaForTest(ctx, p, nil, root, time.Now())
		refused(t, name+": the snapshot", err)
		working, finished, err := store.ExternalActivityBucketsViaForTest(p, nil, time.Now(), func(string) any { return nil })
		refused(t, name+": the buckets", err)
		if work != nil || working != nil || finished != nil {
			t.Fatalf("%s: an answer with the refusal: %v %v %v", name, work, working, finished)
		}
	}
	for name, p := range map[string]*store.RecorderImportPorts{"nothing bound": nil, "an empty binding": {}} {
		log, is, err := store.RecorderImportLogViaForTest(p, []store.PlanChange{{Op: store.OpRecorderImport, Artifact: "sha256:log"}})
		refused(t, name+": whether a proposal is a recorder import", err)
		if log != "" || is {
			t.Fatalf("%s: an answer with the refusal: %q %v", name, log, is)
		}
		refused(t, name+": the landing", store.DecideRecorderImportLogViaForTest(ctx, p, nil, "sha256:log", 1))
	}
	// The positive control: bound ports are called and their answers returned.
	bound := &store.ExternalWorkPorts{
		CheckNote:        func(any) error { return errors.New("note") },
		CheckDescription: func([]byte) error { return errors.New("description") },
		Snapshot: func(context.Context, pgx.Tx, string, time.Time) ([]map[string]any, error) {
			return []map[string]any{{"external_work_id": "ew"}}, nil
		},
		ActivityBuckets: func(work []map[string]any, _ time.Time, _ func(string) any) ([]any, []any) {
			return []any{len(work)}, []any{"done"}
		},
	}
	if err := store.CheckExternalNoteViaForTest(bound, nil); err == nil || err.Error() != "note" {
		t.Fatalf("a bound note check: %v", err)
	}
	if err := store.CheckExternalDescriptionViaForTest(bound, nil); err == nil || err.Error() != "description" {
		t.Fatalf("a bound description check: %v", err)
	}
	if work, err := store.ExternalWorkSnapshotViaForTest(ctx, bound, nil, root, time.Now()); err != nil || len(work) != 1 {
		t.Fatalf("a bound snapshot: %v %v", work, err)
	}
	if working, finished, err := store.ExternalActivityBucketsViaForTest(bound, make([]map[string]any, 3), time.Now(), nil); err != nil ||
		!reflect.DeepEqual(working, []any{3}) || !reflect.DeepEqual(finished, []any{"done"}) {
		t.Fatalf("bound buckets: %v %v %v", working, finished, err)
	}
	rec := &store.RecorderImportPorts{
		LogOf:  func([]store.PlanChange) (string, bool) { return "kept", true },
		Decide: func(context.Context, pgx.Tx, string, int64) error { return errors.New("landed") },
	}
	if log, is, err := store.RecorderImportLogViaForTest(rec, nil); err != nil || log != "kept" || !is {
		t.Fatalf("a bound recorder detect: %q %v %v", log, is, err)
	}
	if err := store.DecideRecorderImportLogViaForTest(ctx, rec, nil, "kept", 1); err == nil || err.Error() != "landed" {
		t.Fatalf("a bound recorder landing: %v", err)
	}
}

// errText is an error's text, "" for none.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the store's wiring binds the external work ports at the package's start (portsbind.go); this holds that binding in place and that each port answers what externalwork and workregistration themselves answer, on a registered work and on documents that are not valid, and that the plan's copy of the collector's actor prefix (it is in the plan view's SQL) is the external work's word.
func TestExternalWorkPortsAreBoundToExternalWork(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	adoptView(t, w, viewDoc(t, w, 1), 0)
	who := wr("thread:external")
	out, err := store.RegisterExternalWork(ctx, w.rt, who, externalwork.ExternalRegistration{Key: "run", Root: root, Node: s1, RunKey: "ports", Artifact: externalSpec(t, w)})
	ok(t, err)
	id := out["external_work_id"].(string)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, externalwork.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	now := time.Now()
	verification := func(id string) any { return "seen " + id }
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		describe := func(ctx context.Context, tx pgx.Tx, id string) (any, error) {
			return externalwork.ExternalDescriptionOf(ctx, tx, id)
		}
		got, err := store.ExternalWorkSnapshotForTest(ctx, tx, root, now)
		if err != nil {
			return err
		}
		want, err := workregistration.Snapshot(ctx, tx, root, now, describe)
		if err != nil {
			return err
		}
		if len(want) != 1 || want[0]["external_work_id"] != id || !reflect.DeepEqual(got, want) {
			t.Errorf("the snapshot port: %v; workregistration: %v", got, want)
		}
		working, finished, err := store.ExternalActivityBucketsForTest(got, now, verification)
		if err != nil {
			return err
		}
		wantWorking, wantFinished := workregistration.ActivityBuckets(want, now, verification)
		if len(wantWorking) != 1 || !reflect.DeepEqual(working, wantWorking) || !reflect.DeepEqual(finished, wantFinished) {
			t.Errorf("the buckets port: %v %v; workregistration: %v %v", working, finished, wantWorking, wantFinished)
		}
		return nil
	}))
	// The note check: the stored record passes; a record of the wrong shape
	// is refused in externalwork's words; an unknown field is refused.
	var raw []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'external_work' FROM agentrt.event WHERE event_type = 'note' AND body_json->'external_work'->>'external_work_id' = $1 ORDER BY event_id DESC LIMIT 1`, id).Scan(&raw))
	var record map[string]any
	ok(t, json.Unmarshal(raw, &record))
	ok(t, store.CheckExternalNoteForTest(record))
	var note externalwork.ExternalNote
	ok(t, json.Unmarshal(raw, &note))
	note.State = "Done"
	if got, want := errText(store.CheckExternalNoteForTest(note)), errText(note.Validate()); want == "" || got != want {
		t.Fatalf("the note check port: %q; externalwork: %q", got, want)
	}
	record["surprise"] = true
	if err := store.CheckExternalNoteForTest(record); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a note with an unknown field: %v", err)
	}
	// The description check, in externalwork's words.
	for _, doc := range []string{`{"v":1,"kind":"agentrt.external_work/v1","task":"t","executor":"codex","provider":"openai","repo":"platform","branch":"b","acceptance_refs":["PV1"],"estimate_minutes":[10,20]}`,
		`{"v":1,"kind":"agentrt.external_work/v1"}`, `not json`} {
		_, want := externalwork.ParseExternalDescription([]byte(doc))
		if got := store.CheckExternalDescriptionForTest([]byte(doc)); errText(got) != errText(want) {
			t.Fatalf("the description check port on %s: %v; externalwork: %v", doc, got, want)
		}
	}
	if _, err := externalwork.ParseExternalDescription([]byte(`not json`)); err == nil {
		t.Fatal("externalwork took a description that is not JSON: the comparison above shows nothing")
	}
	if store.CollectorActorPrefixForTest != externalwork.CollectorActorPrefix {
		t.Fatalf("the plan view excludes actors like %q; a collector's actor starts %q", store.CollectorActorPrefixForTest, externalwork.CollectorActorPrefix)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the recorder import binds its two ports at the package's start (recorderimport.go); this holds that binding in place, that each port answers what the recorder import's own function answers, and that the plan's copies of its card options and stream prefix (the progress projection shows them) are the recorder import's words.
func TestRecorderImportPortsAreBoundToTheRecorderImport(t *testing.T) {
	t.Parallel()
	for _, changes := range [][]store.PlanChange{
		{{Op: store.OpRecorderImport, Artifact: "sha256:log"}},
		{{Op: store.OpRecorderImport}},
		{{Op: "retitle", Node: s1, Title: "x"}},
		{{Op: store.OpRecorderImport, Artifact: "a"}, {Op: store.OpRecorderImport, Artifact: "b"}},
		nil,
	} {
		log, is, err := store.RecorderImportLogForTest(changes)
		ok(t, err)
		if wantLog, wantIs := store.RecorderImportOfForTest(changes); log != wantLog || is != wantIs {
			t.Fatalf("the port on %+v: %q %v; the recorder import: %q %v", changes, log, is, wantLog, wantIs)
		}
	}
	if log, is, _ := store.RecorderImportLogForTest([]store.PlanChange{{Op: store.OpRecorderImport, Artifact: "sha256:log"}}); log != "sha256:log" || !is {
		t.Fatalf("a recorder import is not told: %q %v", log, is)
	}
	// The landing of a log that is not stored: refused in the recorder
	// import's own words, by the port as by the function.
	w := newKernelWorld(t)
	var got, want error
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		got = store.DecideRecorderImportLogForTest(ctx, tx, "sha256:absent", 1)
		want = store.DecideRecorderImportForTest(ctx, tx, "sha256:absent", 1)
		return nil
	}))
	if !errors.Is(want, store.ErrInvalid) || errText(got) != errText(want) {
		t.Fatalf("the landing port: %v; the recorder import: %v", got, want)
	}
	if store.RecorderApplyLabelForTest != store.RecorderApply || store.RecorderRefuseLabelForTest != store.RecorderRefuse ||
		store.RecorderStreamPrefixForTest != store.RecorderStream("") {
		t.Fatalf("the plan's copies %q %q %q are not the recorder import's %q %q %q", store.RecorderApplyLabelForTest, store.RecorderRefuseLabelForTest,
			store.RecorderStreamPrefixForTest, store.RecorderApply, store.RecorderRefuse, store.RecorderStream(""))
	}
}
