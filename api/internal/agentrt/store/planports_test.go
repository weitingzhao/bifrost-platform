//bifrost:owner plan

package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/loopalert"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the rules' side binds the two rule ports at the package's start (envelope.go); this holds that binding in place and that each port answers what the rule store itself answers, with a rule in force and with none.
func TestRulePortsAreBoundToTheRuleStore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	read := func() (plan *planstore.PlanRule, kinds []string, direct [2]*store.StoredRule) {
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
	if plan == nil || *plan != (planstore.PlanRule{ID: direct[0].ID, Version: direct[0].Version, DoneRules: direct[0].Plan.DoneRules}) {
		t.Fatalf("the plan-standing port: %+v; the rule store: %+v", plan, direct[0])
	}
	if !reflect.DeepEqual(kinds, direct[1].CardAuto.Kinds) {
		t.Fatalf("the card-auto-answer port: %v; the rule store: %v", kinds, direct[1].CardAuto.Kinds)
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
	out, err := externalwork.RegisterExternalWork(ctx, w.rt, who, externalwork.ExternalRegistration{Key: "run", Root: root, Node: s1, RunKey: "ports", Artifact: externalSpec(t, w)})
	ok(t, err)
	id := out["external_work_id"].(string)
	_, err = externalwork.PostExternalEvent(ctx, w.rt, who, id, externalwork.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
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
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the recorder import binds its two ports at the package's start (recorderimport.go); this holds that binding in place, that each port answers what the recorder import's own function answers, and that the plan's copies of its card options and stream prefix (the progress projection shows them) are the recorder import's words.
func TestRecorderImportPortsAreBoundToTheRecorderImport(t *testing.T) {
	t.Parallel()
	for _, changes := range [][]planstore.PlanChange{
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
	if log, is, _ := store.RecorderImportLogForTest([]planstore.PlanChange{{Op: store.OpRecorderImport, Artifact: "sha256:log"}}); log != "sha256:log" || !is {
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
		store.RecorderStreamPrefixForTest != externalwork.RecorderStream("") {
		t.Fatalf("the plan's copies %q %q %q are not the recorder import's %q %q %q", store.RecorderApplyLabelForTest, store.RecorderRefuseLabelForTest,
			store.RecorderStreamPrefixForTest, store.RecorderApply, store.RecorderRefuse, externalwork.RecorderStream(""))
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the store's wiring binds the claim port to coord.AlertClaim at the package's start (portsbind.go); this holds that binding in place, that the port writes the alert coord.AlertClaim writes and none for a text that claims nothing, and that the plan's own longest note is coordination's largest message body.
func TestCoordinationPortIsBoundToCoord(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	alerts := func() []string {
		t.Helper()
		rows, err := w.rt.Query(ctx, `SELECT body_json::text || ' cause ' || cause_event_id || ' node ' || plan_node_id FROM agentrt.event WHERE event_type = 'policy_alert' AND event_stream_id = $1 ORDER BY event_id`, planstore.PlanStream(root))
		ok(t, err)
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		ok(t, err)
		return out
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		note, err := store.Append(ctx, tx, store.Event{Stream: planstore.PlanStream(root), Type: "note", Actor: "controller", PlanNode: s1, Body: map[string]any{"text": "a note"}})
		if err != nil {
			return err
		}
		on := store.Event{Stream: planstore.PlanStream(root), PlanNode: s1}
		if err = store.AlertClaimForTest(ctx, tx, on, note.EventID, "plan_note", "", []byte("nothing claimed here")); err != nil {
			return err
		}
		if err = store.AlertClaimForTest(ctx, tx, on, note.EventID, "plan_note", "", []byte("Owner 已批准")); err != nil {
			return err
		}
		return coord.AlertClaim(ctx, tx, on, note.EventID, "plan_note", "", []byte("Owner 已批准"))
	}))
	if got := alerts(); len(got) != 2 || got[0] != got[1] || !strings.Contains(got[0], "approval_claim") {
		t.Fatalf("the port's alert and coord's: %q", got)
	}
	if planstore.MaxNoteText != coord.MaxMessageBody {
		t.Fatalf("a note is at most %d bytes; a message body %d", planstore.MaxNoteText, coord.MaxMessageBody)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the store's wiring binds the size port to questions.BodyBytes at the package's start (portsbind.go), and the plan code keeps its own copies of the question store's body limit and rule channel; this holds the binding in place and each copy equal, so an import's card is not sized by another rule than the store checks it with and a rule's answer is not taken for the Owner's.
func TestQuestionPortAndWordsAreTheQuestionStores(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "plain", "引号 \" 和换行\n", "a\x00b\tc\r\n", strings.Repeat("长", 1200), "<b>&amp;</b> \u2028"} {
		n, err := store.CardBodyBytesForTest(body)
		ok(t, err)
		if want := questions.BodyBytes(body); n != want {
			t.Fatalf("the port counts %q as %d bytes; the question store as %d", body, n, want)
		}
	}
	if questions.BodyBytes("plain") == len("plain") {
		t.Fatal("the question store counts a body as its length: the comparison above shows less than it should")
	}
	if store.QuestionBodyLimitForTest != questions.BodyLimit {
		t.Fatalf("the plan holds a card's body to %d bytes; the question store to %d", store.QuestionBodyLimitForTest, questions.BodyLimit)
	}
	if store.RuleAnswerChannelForTest != questions.RuleChannel {
		t.Fatalf("the plan reads a rule's answer by channel %q; the question store writes %q", store.RuleAnswerChannelForTest, questions.RuleChannel)
	}
}

// Origin: 2026-11-17, plan core wall step 5 (design duty/from-biz-2026-11-17-plan-core-wall.md): the loop alerts bind the port at the package's start (loopalert.go); this holds that binding in place: the port lists no loop while none has an alert, the loop an alert was recorded for with the error it kept, and no loop again once a note says it passes.
func TestLoopPortIsBoundToTheLoopAlerts(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	failing := func() []map[string]any {
		t.Helper()
		var out []map[string]any
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			out, err = store.ControllerLoopsFailingForTest(ctx, tx)
			return err
		}))
		return out
	}
	if got := failing(); len(got) != 0 {
		t.Fatalf("with no alert: %v", got)
	}
	loop := "agentrt-facts-rules" // one of the controller's loops (loopalert.ControllerLoops)
	ok(t, loopalert.AlertFailingLoop(ctx, w.rt, loop, loopalert.FailuresToAlert, "the check constraint is missing"))
	got := failing()
	if len(got) != 1 || got[0]["loop"] != loop || !strings.Contains(got[0]["error"].(string), "the check constraint is missing") {
		t.Fatalf("with one alert: %v", got)
	}
	ok(t, loopalert.NoteLoopPasses(ctx, w.rt, loop, loopalert.FailuresToAlert))
	if got := failing(); len(got) != 0 {
		t.Fatalf("after the loop passes again: %v", got)
	}
}
