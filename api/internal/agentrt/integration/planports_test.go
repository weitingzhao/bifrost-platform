//bifrost:owner plan

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/loopalert"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planwiring"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// The store binds the plan domain's ports at its start. These tests hold each
// binding to its real provider from outside the plan: through what the plan
// offers (proposals, the decision pass, notes, the plan view, Progress) and
// the checks it declares to the event log and the artifact store.

// The options of a recorder import's card, and the stream a log goes to.
const (
	recorderApplyWords   = "Import the log"
	recorderRefuseWords  = "Do not import it"
	recorderStreamPrefix = "legacy:recorder:"
)

// preparedLog is a recorder log as lineage hands it to the plan, with the
// card body the test wants asked about; nothing of it is read from a store.
func preparedLog(work, body string) planstore.RecorderLog {
	return planstore.RecorderLog{Work: work, Stream: recorderStreamPrefix + work, LogID: noSuch, Doc: []byte(`{"fixture":"` + work + `"}`),
		CardBody:  func(string, string, string) string { return body },
		Imported:  func(context.Context, pgx.Tx) (int64, string, error) { return 0, "", nil },
		TryImport: func(context.Context, pgx.Tx) error { return nil }}
}

// lastCard is the card the asker was last asked.
func lastCard(t *testing.T, a *asker) planstore.Card {
	t.Helper()
	if len(a.cards) == 0 {
		t.Fatal("no card was asked")
	}
	return a.cards[len(a.cards)-1]
}

// errText is an error's text, "" for none.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the store's wiring binds the external work ports at the package's start (portsbind.go); this holds that binding in place and that each port answers what externalwork and workregistration themselves answer, on a registered work and on documents that are not valid. 2026-11-18 (TD-582): read through the plan view, the event log's check of a plan_view_adopted body and the artifact store's check of a body, which is where the plan calls these ports.
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
	view, err := planstore.PlanViewPage(ctx, w.rt, root, "")
	ok(t, err)
	now, err := time.Parse(eventlog.TimeLayout, view["as_of"].(string))
	ok(t, err)
	// No view binds this work to a verification, so the plan view says Unknown.
	verification := func(string) any { return "Unknown" }
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		describe := func(ctx context.Context, tx pgx.Tx, id string) (any, error) {
			return externalwork.ExternalDescriptionOf(ctx, tx, id)
		}
		activity, err := workregistration.ReadRootActivity(ctx, tx, root, now, workregistration.RecentPage{Limit: 21}, describe)
		if err != nil {
			return err
		}
		want := activity.Work
		if len(want) != 1 || want[0]["external_work_id"] != id {
			t.Errorf("workregistration: %v", want)
		}
		wantWorking, wantFinished := workregistration.ActivityBuckets(want, now, verification)
		if got := view["external_work"]; !reflect.DeepEqual(got, want) {
			t.Errorf("the snapshot port: %v; workregistration: %v", got, want)
		}
		if len(wantWorking) != 1 || !reflect.DeepEqual(view["working"], wantWorking) || !reflect.DeepEqual(view["finished_today"], wantFinished) {
			t.Errorf("the buckets port: %v %v; workregistration: %v %v", view["working"], view["finished_today"], wantWorking, wantFinished)
		}
		// The port hands on the caller's verification function, whatever it
		// answers. 2026-11-18, TD-582: the plan's own only ever says Unknown.
		seen := func(id string) any { return "seen " + id }
		working, _ := planwiring.ExternalWorkPorts().ActivityBuckets(want, now, seen)
		if len(working) != 1 || working[0].(map[string]any)["verification"] != "seen "+id {
			t.Errorf("the buckets port with another verification: %v", working)
		}
		return nil
	}))
	// Origin: 2026-11-18, TD-582 (inventory §2.3): the plan stopped reading the work's records with SQL of its own; each read port must answer what the lineage answers, in the caller's transaction, and the plan view must show it. A heartbeat is recorded and never listed (the 2026-10-26 finding of TestPlanViewRecentSkipsHeartbeats).
	_, err = externalwork.PostExternalEvent(ctx, w.rt, who, id, externalwork.ExternalEvent{Key: "beat", Seq: 2, Type: "heartbeat"})
	ok(t, err)
	ports := planwiring.ExternalWorkPorts()
	stream := "external-work:" + id
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		// Origin: 2026-11-18, the plan view went from 371 to 485 ms on PROD R2 after b6c6abc2 split one read into several scans; the streams, the page and the registrants now come from one read, which must answer as the separate ports did.
		read := func(root string, page planstore.RecentWorkPage) planstore.RootActivity {
			t.Helper()
			got, err := ports.RootActivity(ctx, tx, root, now, page)
			ok(t, err)
			lineage, err := workregistration.ReadRootActivity(ctx, tx, root, now, workregistration.RecentPage(page), func(ctx context.Context, tx pgx.Tx, id string) (any, error) {
				return externalwork.ExternalDescriptionOf(ctx, tx, id)
			})
			ok(t, err)
			if !reflect.DeepEqual(got.Work, lineage.Work) || !reflect.DeepEqual(got.Streams, lineage.Streams) || !reflect.DeepEqual(got.Registrants, lineage.Registrants) || len(got.Recent) != len(lineage.Recent) {
				t.Errorf("the activity port: %v; the lineage: %v", got, lineage)
			}
			for i, e := range got.Recent {
				if i < len(lineage.Recent) && (string(e.Raw) != string(lineage.Recent[i].Raw) || e.ID != lineage.Recent[i].ID) {
					t.Errorf("the activity port: %s; the lineage: %v", e.Raw, lineage.Recent)
				}
			}
			return got
		}
		bounds := []byte(`{"` + stream + `":2}`)
		listed := func(before int64, limit int) []int64 {
			t.Helper()
			out := []int64{}
			for _, e := range read(root, planstore.RecentWorkPage{Bounds: bounds, Before: before, Limit: limit}).Recent {
				out = append(out, e.ID)
			}
			return out
		}
		first := read(root, planstore.RecentWorkPage{Limit: 21})
		if want := map[string]int64{stream: 2}; !reflect.DeepEqual(first.Streams, want) {
			t.Errorf("the recent streams: %v; want %v", first.Streams, want)
		}
		if !reflect.DeepEqual(first.Registrants, []string{who.Actor}) {
			t.Errorf("the registrants: %v", first.Registrants)
		}
		all := listed(0, 21)
		if len(all) != 2 || all[0] <= all[1] {
			t.Fatalf("the work's listed events, newest first: %v", all)
		}
		// The first page, with no bounds, is the page bounded by its own streams.
		if len(first.Recent) != 2 || first.Recent[0].ID != all[0] || first.Recent[1].ID != all[1] {
			t.Errorf("the first page: %v; want %v", first.Recent, all)
		}
		for _, c := range []struct {
			before int64
			limit  int
			want   []int64
		}{{all[0], 21, all[1:]}, {0, 1, all[:1]}, {0, 0, []int64{}}} {
			if got := listed(c.before, c.limit); !reflect.DeepEqual(got, c.want) {
				t.Errorf("before %d limit %d: %v; want %v", c.before, c.limit, got, c.want)
			}
		}
		// Only what the bounds hold, and only this root's; the streams and the
		// registrants do not depend on the page.
		for _, c := range []struct {
			root, bounds string
			want         int
		}{{root, `{"` + stream + `":1}`, 1}, {root, `{}`, 0}, {"R-none", string(bounds), 0}} {
			got := read(c.root, planstore.RecentWorkPage{Bounds: []byte(c.bounds), Limit: 21})
			if len(got.Recent) != c.want || (c.want == 1 && got.Recent[0].ID != all[1]) {
				t.Errorf("root %s within %s: %v; want %d of them", c.root, c.bounds, got.Recent, c.want)
			}
			if c.root == root && (!reflect.DeepEqual(got.Streams, first.Streams) || !reflect.DeepEqual(got.Registrants, first.Registrants)) {
				t.Errorf("within %s: %v", c.bounds, got)
			}
		}
		if none := read("R-none", planstore.RecentWorkPage{Limit: 21}); len(none.Streams) != 0 || len(none.Recent) != 0 || none.Registrants == nil || len(none.Registrants) != 0 {
			t.Errorf("the activity of a plan with no work: %v", none)
		}
		for r, want := range map[string]bool{root: true, "R-none": false} {
			if has, err := ports.HasWork(ctx, tx, r); err != nil || has != want {
				t.Errorf("has work under %s: %v %v; want %v", r, has, err, want)
			}
		}
		return nil
	}))
	view, err = planstore.PlanViewPage(ctx, w.rt, root, "")
	ok(t, err)
	var recent []struct {
		Stream string `json:"event_stream_id"`
		Seq    int64  `json:"seq"`
	}
	ok(t, json.Unmarshal(view["recent_changes"].(json.RawMessage), &recent))
	var workSeqs []int64
	for _, e := range recent {
		if e.Stream == stream {
			workSeqs = append(workSeqs, e.Seq)
		}
	}
	if want := []int64{2, 1}; !reflect.DeepEqual(workSeqs, want) {
		t.Fatalf("the work's events in Recent changes: %v; want %v", workSeqs, want)
	}
	// The registrant acted on the plan's tree and is not unregistered work;
	// the same thread with no work of this plan is.
	unregistered := func() any { return view["coverage"].(map[string]any)["unregistered_work"] }
	if got := unregistered(); got != 0 {
		t.Fatalf("unregistered work with the thread registered: %v", got)
	}
	// A plan with no adopted view: a business mission may be proposed until
	// work is registered under it, and is refused after (its dev boundary).
	bare := newKernelWorld(t)
	bare.legacy(t, `UPDATE agentrt.mission SET tenant='business' WHERE mission_id='M-2'`)
	bind := func(key string) error {
		_, err := planstore.ProposePlanChange(ctx, bare.rt, wr("thread:pv"), key, root, "bind", []planstore.PlanChange{{Op: "rebind_step", Node: s2, MissionID: "M-2"}}, (&asker{}).ask)
		return err
	}
	ok(t, bind("no-work"))
	_, err = externalwork.RegisterExternalWork(ctx, bare.rt, who, externalwork.ExternalRegistration{Key: "run", Root: root, Node: s1, RunKey: "ports", Artifact: externalSpec(t, bare)})
	ok(t, err)
	is(t, bind("with-work"), eventlog.ErrInvalid)
	// The rows from before the record had its own type (migration 0032) stay
	// and count the same.
	admin := bare.db.Connect(t, bare.db.Admin)
	for _, q := range []string{`SET session_replication_role=replica`, `UPDATE agentrt.event SET event_type='note' WHERE event_type='external_work_noted'`, `SET session_replication_role=DEFAULT`} {
		_, err = admin.Exec(ctx, q)
		ok(t, err)
	}
	is(t, bind("with-old-work"), eventlog.ErrInvalid)
	// On the first plan, a thread that acted on the tree and registered
	// nothing there does count.
	stranger := wr("thread:unregistered")
	_, err = planstore.ProposePlanChange(ctx, w.rt, stranger, "stranger", root, "retitle", []planstore.PlanChange{{Op: "retitle", Node: s3, Title: "new"}}, (&asker{}).ask)
	ok(t, err)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream(root), Type: "note", Actor: stranger.Actor, PlanNode: s2, Body: map[string]any{"text": "on the tree"}})
		return err
	}))
	view, err = planstore.PlanViewPage(ctx, w.rt, root, "")
	ok(t, err)
	if got := unregistered(); got != 1 {
		t.Fatalf("unregistered work with a thread that registered nothing: %v", got)
	}
	// A full page of the plan's own events does not push out a newer event of
	// the work that falls among them.
	ownNotes := func(n int) {
		t.Helper()
		for i := range n {
			ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream(root), Type: "note", Actor: stranger.Actor, PlanNode: s2, Body: map[string]any{"text": fmt.Sprint("own ", i)}})
				return err
			}))
		}
	}
	ownNotes(25)
	_, err = externalwork.PostExternalEvent(ctx, w.rt, who, id, externalwork.ExternalEvent{Key: "among", Seq: 3, Type: "submitted"})
	ok(t, err)
	ownNotes(5)
	view, err = planstore.PlanViewPage(ctx, w.rt, root, "")
	ok(t, err)
	recent = nil
	ok(t, json.Unmarshal(view["recent_changes"].(json.RawMessage), &recent))
	among := false
	for _, e := range recent {
		among = among || (e.Stream == stream && e.Seq == 4)
	}
	if !among {
		t.Fatalf("the work's event among the plan's own is not listed: %+v", recent)
	}
	// The note check, which the plan runs on a view's event before its body:
	// a record it passes goes on to the body's own first complaint, that this
	// body has no text.
	checkNote := func(record any) error {
		return eventlog.CheckBody("plan_view_adopted", map[string]any{"external_work": record})
	}
	const passed = `store: plan_view_adopted needs "text"`
	// The stored record passes; a record of the wrong shape is refused in
	// externalwork's words; an unknown field is refused.
	var raw []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'external_work' FROM agentrt.event WHERE event_type = 'external_work_noted' AND body_json->'external_work'->>'external_work_id' = $1 ORDER BY event_id DESC LIMIT 1`, id).Scan(&raw))
	var record map[string]any
	ok(t, json.Unmarshal(raw, &record))
	if got := errText(checkNote(record)); got != passed {
		t.Fatalf("the stored record: %q", got)
	}
	var note externalwork.ExternalNote
	ok(t, json.Unmarshal(raw, &note))
	note.State = "Done"
	if got, want := errText(checkNote(note)), errText(note.Validate()); want == "" || got != want {
		t.Fatalf("the note check port: %q; externalwork: %q", got, want)
	}
	record["surprise"] = true
	if err := checkNote(record); !errors.Is(err, eventlog.ErrInvalid) {
		t.Fatalf("a note with an unknown field: %v", err)
	}
	// The description check, in externalwork's words, as the artifact store
	// asks it of a body that says it is a description. A body that is not
	// JSON at all says nothing and never comes here; two "kind" keys read as
	// one to the store's look at the kind, and are not JSON to externalwork.
	const kind = `"kind":"agentrt.external_work/v1"`
	valid := `{"v":1,` + kind + `,"task":"t","executor":"codex","provider":"openai","repo":"platform","branch":"b","acceptance_refs":["PV1"],"estimate_minutes":[10,20]}`
	for _, c := range []struct {
		doc, says string
	}{{valid, ""}, {`{"v":1,` + kind + `}`, "invalid"}, {`{` + kind + `,` + kind + `}`, "invalid JSON"}, {`{` + kind + `,"surprise":1}`, "surprise"}} {
		_, want := externalwork.ParseExternalDescription([]byte(c.doc))
		if (want == nil) != (c.says == "") || !strings.Contains(errText(want), c.says) {
			t.Fatalf("externalwork on %s: %v; the comparison below would show less than it should", c.doc, want)
		}
		var got error
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, got = artifact.PutArtifact(ctx, tx, "controller", "other", "application/json", []byte(c.doc))
			return nil
		}))
		if errText(got) != errText(want) {
			t.Fatalf("the description check port on %s: %v; externalwork: %v", c.doc, got, want)
		}
	}
	// Text that is not JSON at all: the artifact store never asks about it, so
	// the port is asked directly. 2026-11-18, TD-582: the case this test had
	// before it went through the plan's contract.
	_, notJSON := externalwork.ParseExternalDescription([]byte(`not json`))
	if notJSON == nil {
		t.Fatal("externalwork took a description that is not JSON: the comparison below shows nothing")
	}
	if got := planwiring.ExternalWorkPorts().CheckDescription([]byte(`not json`)); errText(got) != errText(notJSON) {
		t.Fatalf("the description check port on text that is not JSON: %v; externalwork: %v", got, notJSON)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the recorder import binds its port at the package's start (portsbind.go); this holds that binding in place, that the port answers what the recorder import's own function answers, and that the plan's copies of its card options and stream prefix (the progress projection shows them) are the recorder import's words. 2026-11-18 (TD-582): read through a proposal, its card in needs_you and the decision pass; which changes are a recorder import is the plan's own and is held in planstore (TestARecorderImportIsItsOneChange).
func TestRecorderImportPortsAreBoundToTheRecorderImport(t *testing.T) {
	t.Parallel()
	pool := recorderDB(t)
	a := &asker{}
	// A log lineage really prepared: its stream, read back through the plan's
	// prefix, is the work again, and the card shows the two options.
	out, err := proposeRecorder(ctx, pool, wr("thread:s1"), "real", "retire", tdLog(t), a.ask)
	ok(t, err)
	options := "[" + recorderApplyWords + " " + recorderRefuseWords + "]"
	if got := fmt.Sprint(lastCard(t, a).Options); got != options {
		t.Fatalf("the card asked with %s; want %s", got, options)
	}
	card := recorderCard(t, pool, out["question_id"].(string))
	if card == nil || card["title"] != "Import recorder log td-336: retire" {
		t.Fatalf("needs_you item %v", card)
	}
	action, _ := card["action"].(map[string]any)
	if got := fmt.Sprint(action["options"]); got != options {
		t.Fatalf("needs_you shows %s; want %s", got, options)
	}
	// The landing of a log that is not stored: refused in the recorder
	// import's own words, and the decision records them.
	out, err = planstore.ProposeRecorderImport(ctx, pool, wr("thread:s1"), "absent", "retire", preparedLog("absent", "a body"), a.ask)
	ok(t, err)
	a.answer(out["question_id"].(string), seenfacts.QuestionAnswered, planstore.CardApplyKey)
	_, err = planstore.DecidePlanChanges(ctx, pool, a.question, nil)
	ok(t, err)
	var want error
	ok(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		want = externalwork.DecideRecorderImport(ctx, tx, noSuch, 1)
		return nil
	}))
	var decision, got string
	ok(t, pool.QueryRow(ctx, `SELECT body_json->>'decision', COALESCE(body_json->>'error', '') FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'plan_change_decided'`,
		recorderStreamPrefix+"absent").Scan(&decision, &got))
	if !errors.Is(want, eventlog.ErrInvalid) || decision != "failed" || got != errText(want) {
		t.Fatalf("the landing port: %s %q; the recorder import: %v", decision, got, want)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the store's wiring binds the claim port to coord.AlertClaim at the package's start (portsbind.go); this holds that binding in place, that the port writes the alert coord.AlertClaim writes and none for a text that claims nothing, and that the plan's own longest note is coordination's largest message body. 2026-11-18 (TD-582): read through PostPlanNote, which is where the plan calls the port.
func TestCoordinationPortIsBoundToCoord(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.arch(t, [2]string{"ARCH:state-db", ""})
	stream := planstore.PlanStream("ARCH")
	alerts := func() []string {
		t.Helper()
		rows, err := w.rt.Query(ctx, `SELECT body_json::text || ' cause ' || cause_event_id || ' node ' || plan_node_id FROM agentrt.event WHERE event_type = 'policy_alert' AND event_stream_id = $1 ORDER BY event_id`, stream)
		ok(t, err)
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		ok(t, err)
		return out
	}
	_, err := planstore.PostPlanNote(ctx, w.rt, wr("thread:s1"), "clean", "ARCH:state-db", "nothing claimed here")
	ok(t, err)
	if got := alerts(); len(got) != 0 {
		t.Fatalf("a note that claims nothing raised %q", got)
	}
	const claim = "Owner 已批准"
	note, err := planstore.PostPlanNote(ctx, w.rt, wr("thread:s1"), "claim", "ARCH:state-db", claim)
	ok(t, err)
	cause, err := strconv.ParseInt(fmt.Sprint(note["event_id"]), 10, 64)
	ok(t, err)
	// What coordination itself writes for that note, beside what the port did.
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return coord.AlertClaim(ctx, tx, eventlog.Event{Stream: stream, PlanNode: "ARCH:state-db"}, cause, "plan_note", "", []byte(claim))
	}))
	if got := alerts(); len(got) != 2 || got[0] != got[1] || !strings.Contains(got[0], "approval_claim") {
		t.Fatalf("the port's alert and coord's: %q", got)
	}
	if planstore.MaxNoteText != 8<<10 {
		t.Fatalf("a note is at most %d bytes; a message body %d", planstore.MaxNoteText, 8<<10)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the store's wiring binds the size port to questions.BodyBytes at the package's start (portsbind.go), and the plan code keeps its own copies of the question store's body limit and rule channel; this holds the binding in place and each copy equal, so an import's card is not sized by another rule than the store checks it with and a rule's answer is not taken for the Owner's. 2026-11-18 (TD-582): read through a recorder import's proposal, which sizes its card, and the decision pass, which reads who answered.
func TestQuestionPortAndWordsAreTheQuestionStores(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := &asker{}
	// sized is body grown to size bytes as the question store counts them; a
	// body already past size stays as it is. The first letter goes on alone:
	// it can bring white space the store trims from the end into the count.
	sized := func(body string, size int) string {
		t.Helper()
		if questions.BodyBytes(body) < size {
			body += "a"
		}
		for n := questions.BodyBytes(body); n < size; n = questions.BodyBytes(body) {
			body += strings.Repeat("a", size-n)
		}
		return body
	}
	if questions.BodyBytes("plain") == len("plain") {
		t.Fatal("the question store counts a body as its length: the comparison below shows less than it should")
	}
	for i, body := range []string{"", "plain", "引号 \" 和换行\n", "a\x00b\tc\r\n", strings.Repeat("长", 1200), "<b>&amp;</b> \u2028"} {
		work := fmt.Sprintf("size-%d", i)
		// Past the limit, as the store counts (by one byte where the body
		// allows): refused, and the refusal says that size and the store's limit.
		over := sized(body, questions.BodyLimit+1)
		_, err := planstore.ProposeRecorderImport(ctx, w.rt, wr("thread:s1"), work+"-over", "retire", preparedLog(work, over), a.ask)
		want := fmt.Sprintf("would be %d bytes, more than the question store's %d", questions.BodyBytes(over), questions.BodyLimit)
		if !errors.Is(err, eventlog.ErrInvalid) || !strings.Contains(errText(err), want) {
			t.Fatalf("a body of %q past the limit: %v; want %q", body, err, want)
		}
		if questions.BodyBytes(body) > questions.BodyLimit {
			continue
		}
		// At the limit exactly: asked.
		fits := sized(body, questions.BodyLimit)
		if questions.BodyBytes(fits) != questions.BodyLimit {
			t.Fatalf("a body of %q does not come to the limit: %d", body, questions.BodyBytes(fits))
		}
		_, err = planstore.ProposeRecorderImport(ctx, w.rt, wr("thread:s1"), work+"-fits", "retire", preparedLog(work, fits), a.ask)
		ok(t, err)
		if lastCard(t, a).Body != fits {
			t.Fatalf("a body of %q at the limit was not the card asked", body)
		}
	}
	// A rule's answer, on the channel the question store writes it on, is the
	// rule's in the decision; any other answer is the Owner's.
	const rule = "rule card-auto-answer v3"
	propose := func(key, channel, by string) string {
		t.Helper()
		out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "rename", []planstore.PlanChange{{Op: "retitle", Node: item, Title: key}}, a.ask)
		ok(t, err)
		id := out["question_id"].(string)
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.answers == nil {
			a.answers = map[string]seenfacts.Question{}
		}
		a.answers[id] = seenfacts.Question{ID: id, Status: seenfacts.QuestionAnswered, Option: "b", AnsweredBy: by, AnsweredAt: "2026-10-14T20:00:00Z", Channel: channel}
		return id
	}
	byRule, byOwner := propose("by-rule", questions.RuleChannel, rule), propose("by-owner", "", "owner")
	_, err := planstore.DecidePlanChanges(ctx, w.rt, a.question, nil)
	ok(t, err)
	for id, want := range map[string]string{byRule: rule, byOwner: "owner"} {
		var by string
		ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'by' FROM agentrt.event WHERE event_type = 'plan_change_decided' AND body_json->>'question_id' = $1`, id).Scan(&by))
		if by != want {
			t.Fatalf("question %s was decided by %q; want %q", id, by, want)
		}
	}
}

// Origin: 2026-11-17, plan core wall step 5 (design duty/from-biz-2026-11-17-plan-core-wall.md): the loop alerts bind the port at the package's start (loopalert.go); this holds that binding in place: the port lists no loop while none has an alert, the loop an alert was recorded for with the error it kept, and no loop again once a note says it passes. 2026-11-18 (TD-582): read through Progress's controller_alerts, which is what the port is for.
func TestLoopPortIsBoundToTheLoopAlerts(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	failing := func() []map[string]any {
		t.Helper()
		progress, err := planstore.ProgressSince(ctx, w.rt, time.Time{})
		ok(t, err)
		out, isList := progress["controller_alerts"].([]map[string]any)
		if !isList {
			t.Fatalf("controller_alerts: %#v", progress["controller_alerts"])
		}
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

// importDB is a seeded database and the plan import of the ledger fixture.
func importDB(t *testing.T) (*pgxpool.Pool, planimport.Request) {
	t.Helper()
	db := seededDB(t)
	text, err := os.ReadFile("../planimport/testdata/ledger.md")
	ok(t, err)
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	return db.Pool(t, db.Runtime), req
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the rules' side binds the two rule ports at the package's start (rules.go); this holds that binding in place and that each port answers what the rule store itself answers, with a rule in force and with none. 2026-11-18 (TD-582): read through what the rules decide in a proposal (the standing authorization and its "by", a card's ByRule), so the ports' functions need not be exported.
func TestRulePortsAreBoundToTheRuleStore(t *testing.T) {
	t.Parallel()
	direct := func(w *world) (plan, auto *store.StoredRule) {
		t.Helper()
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			if plan, err = store.CurrentRule(ctx, tx, rules.KindPlan, time.Now()); err != nil {
				return err
			}
			auto, err = store.CurrentRule(ctx, tx, rules.KindCardAuto, time.Now())
			return err
		}))
		return plan, auto
	}
	a := &asker{}
	// A first done rule naming the step's own task: what the plan-standing
	// rule grants, and only it.
	doneRule := func(w *world, key string) map[string]any {
		t.Helper()
		out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "test", []planstore.PlanChange{{Op: "set_done_rule", Node: s2,
			Rule: []byte(`{"task_accepted":"t_01M5192P0JPMJAP63ZKRPXJ2VV"}`)}}, a.ask)
		ok(t, err)
		return out
	}
	// Whether each kind of card is asked by rule, against the kinds the rule
	// store has in force: one kind a rule can list, one it does not here.
	cardsAskedAsTheRuleSays := func(w *world, key string, kinds []string) {
		t.Helper()
		_, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "rename", []planstore.PlanChange{{Op: "retitle", Node: item, Title: key}}, a.ask)
		ok(t, err)
		if got, want := lastCard(t, a).ByRule, slices.Contains(kinds, "plan_change"); got != want {
			t.Fatalf("%s: a plan change's card asked by rule %v; the rule store's kinds %v", key, got, kinds)
		}
		_, err = planstore.ProposeRecorderImport(ctx, w.rt, wr("thread:s1"), key, "retire", preparedLog(key, "a body"), a.ask)
		ok(t, err)
		if got, want := lastCard(t, a).ByRule, slices.Contains(kinds, "recorder_import"); got != want {
			t.Fatalf("%s: a recorder import's card asked by rule %v; the rule store's kinds %v", key, got, kinds)
		}
	}
	// No rule in force: the ports say none, as the rule store does; a port
	// that refused would fail the proposals.
	w := newWorld(t)
	if plan, auto := direct(w); plan != nil || auto != nil {
		t.Fatalf("with no rule: %+v %+v", plan, auto)
	}
	if out := doneRule(w, "none"); out["decision"] != "" {
		t.Fatalf("with no rule in force, decided at once: %v", out)
	}
	cardsAskedAsTheRuleSays(w, "no-rule", nil)
	// A rule in force that does not grant done rules grants nothing.
	putPlanStanding(t, w, false, false)
	if plan, _ := direct(w); plan == nil || plan.Plan.DoneRules {
		t.Fatalf("the fixture rule: %+v", plan)
	}
	if out := doneRule(w, "not-granted"); out["decision"] != "" {
		t.Fatalf("under a rule without done_rules, decided at once: %v", out)
	}
	// Rules in force: the decision names the rule the store has, and cards
	// are asked by rule for the kinds it lists.
	w = newWorld(t)
	// The whole list of kinds, as the rule store has it, including kinds the
	// plan never asks about: what follows through the plan sees only its own
	// three. 2026-11-18, TD-582: the list comparison this test had before it
	// went through the plan's contract.
	kindsInForce := func() []string {
		t.Helper()
		var kinds []string
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			var r *store.StoredRule
			r, err = store.InForceIn(ctx, tx, rules.KindCardAuto)
			if r != nil {
				kinds = r.CardAuto.Kinds
			}
			return err
		}))
		return kinds
	}
	if kinds := kindsInForce(); kinds != nil {
		t.Fatalf("with no rule in force, the card-auto port: %v", kinds)
	}
	putPlanStanding(t, w, true, false)
	signCardAutoSeconds(t, w.rt, "5", "[blocked, mission, plan_change]")
	plan, auto := direct(w)
	if kinds := kindsInForce(); auto == nil || !reflect.DeepEqual(kinds, auto.CardAuto.Kinds) || !slices.Contains(kinds, "blocked") || !slices.Contains(kinds, "mission") {
		t.Fatalf("the card-auto port: %v; the rule store: %+v", kinds, auto)
	}
	if plan == nil || auto == nil || len(auto.CardAuto.Kinds) == 0 || !plan.Plan.DoneRules {
		t.Fatalf("the fixture rules are not in force: %+v %+v", plan, auto)
	}
	if out := doneRule(w, "granted"); out["decision"] != "applied" {
		t.Fatalf("under the rule, a first done rule: %v", out)
	}
	var by string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'by' FROM agentrt.event WHERE event_type = 'plan_change_decided' ORDER BY event_id DESC LIMIT 1`).Scan(&by))
	if want := fmt.Sprintf("standing authorization (%s v%d)", plan.ID, plan.Version); by != want {
		t.Fatalf("decided by %q; the rule store has %q", by, want)
	}
	cardsAskedAsTheRuleSays(w, "listed", auto.CardAuto.Kinds)
	// The third kind the plan asks about, a plan import, with no rule, under a
	// rule that does not list it and under one that does (a database each: one
	// import of a plan waits at a time).
	for _, c := range []struct {
		kinds string
		want  bool
	}{{"", false}, {"[blocked]", false}, {"[plan_import]", true}} {
		pool, req := importDB(t)
		if c.kinds != "" {
			signCardAutoSeconds(t, pool, "5", c.kinds)
		}
		cards := &asker{}
		_, err := planstore.ProposePlanImport(ctx, pool, wr("thread:s1"), "import", "import", req, cards.ask)
		ok(t, err)
		if got := lastCard(t, cards).ByRule; got != c.want {
			t.Fatalf("under kinds %q: a plan import's card asked by rule %v", c.kinds, got)
		}
	}
}
