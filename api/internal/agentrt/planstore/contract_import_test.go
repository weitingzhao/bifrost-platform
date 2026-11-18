package planstore_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Plan imports and recorder-log imports: through the contract (doc.go).

func (w *planWorld) proposeImport(key string, req planimport.Request) (map[string]any, error) {
	return planstore.ProposePlanImport(contractCtx, w.rt, writer, key, "bring it in", req, w.desk.ask)
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestImportDecisionOnce's setting and agentrtapi's import route tests: an import is tried, kept and put on one card, and lands only on the first option, under that decision, as the plan the ledger says; the wrong outcomes are a plan appearing unapproved, or steps imported in the wrong state.
func TestContractImportLandsOnTheCard(t *testing.T) {
	t.Parallel()
	w := emptyWorld(t)
	req := roundRequest(t)
	out, err := w.proposeImport("k1", req)
	must(t, err)
	q := w.questionOf(out)
	c := w.desk.last()
	if out["decision"] != "" || out["request_artifact_id"] == nil || !strings.HasPrefix(c.Title, "Import plan R2: ") ||
		len(c.Options) != 2 || c.Options[0] != "Import the plan" || c.OwnerDuty != "irreversible" || c.ByRule {
		t.Fatalf("answer %v, card %+v", out, c)
	}
	if w.count(`SELECT count(*) FROM agentrt.plan_node`) != 0 || w.events("plan:R2", "legacy_imported") != 0 {
		t.Fatal("the trial left the plan behind")
	}
	// Asking again, under another key, waits on the same card.
	again, err := w.proposeImport("k2", req)
	must(t, err)
	if again["question_id"] != q || w.desk.count() != 1 {
		t.Fatalf("asking again: %v, %d cards", again, w.desk.count())
	}
	// Another request for the same root is refused while this one waits.
	other := req
	other.Source.Commit = strings.Repeat("b", 40)
	_, err = w.proposeImport("k3", other)
	wantIs(t, "another request waiting", err, eventlog.ErrInvalid)

	w.applyCard(out)
	if got := w.decisionOf(out); got != "applied" {
		t.Fatalf("decided %q: %s", got, w.decisionError(out))
	}
	if n := w.count(`SELECT count(*) FROM agentrt.plan_node`); n != 1+len(req.Nodes) {
		t.Fatalf("%d nodes; want %d", n, 1+len(req.Nodes))
	}
	for id, want := range map[string]string{stepLive: "active", stepA: "planned", stepDone: "done", stepC: "planned"} {
		if got := w.node(id).state; got != want {
			t.Errorf("%s is %s; want %s", id, got, want)
		}
	}
	var head int64
	must(t, w.rt.QueryRow(contractCtx, `SELECT h.event_id FROM agentrt.event h JOIN agentrt.event d ON h.cause_event_id = d.event_id
	  WHERE h.event_type = 'legacy_imported' AND h.event_stream_id = 'plan:R2' AND d.event_type = 'plan_change_decided'`).Scan(&head))
	// The done step is done by its manual rule, with its evidence stored.
	if n := w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'plan_step_marked_done' AND plan_node_id = $1 AND jsonb_array_length(body_json->'evidence') = 2`, stepDone); n != 1 {
		t.Fatalf("%d manual records with evidence on the done step", n)
	}
	var cause string
	w.scalar(&cause, `SELECT body_json->>'cause' FROM agentrt.event WHERE event_type = 'plan_node_state_changed' AND plan_node_id = $1 ORDER BY event_id DESC LIMIT 1`, stepDone)
	if cause != "done_rule_manual" {
		t.Fatalf("the done step's cause is %q", cause)
	}
	// The same request again answers what happened, with no card.
	before, cards := w.footprint(), w.desk.count()
	done, err := w.proposeImport("k4", req)
	must(t, err)
	if done["decision"] != "applied" || done["imported"] != false || fmt.Sprint(done["event_id"]) != fmt.Sprint(head) || w.desk.count() != cards {
		t.Fatalf("after the import: %v", done)
	}
	if w.count(`SELECT count(*) FROM agentrt.plan_node`) != 1+len(req.Nodes) || w.events("plan:R2", "legacy_imported") != 1 {
		t.Fatalf("the plan changed (%s)", before)
	}
	_, err = w.proposeImport("k5", other)
	wantIs(t, "another request for a plan that exists", err, eventlog.ErrInvalid)
}

// Origin: 2026-11-18, TD-582: the refusals of a plan import through its only production entry (ProposePlanImport); the wrong outcomes are a request kept as an artifact without its card, or a refused import that consumes the plan's name.
func TestContractImportRefusals(t *testing.T) {
	t.Parallel()
	w := emptyWorld(t)
	req := roundRequest(t)
	bad := req
	bad.Format = "something else"
	before := w.footprint()
	for name, call := range map[string]func() (map[string]any, error){
		"no key": func() (map[string]any, error) {
			return planstore.ProposePlanImport(contractCtx, w.rt, writer, "", "r", req, w.desk.ask)
		},
		"no reason": func() (map[string]any, error) {
			return planstore.ProposePlanImport(contractCtx, w.rt, writer, "k", "  ", req, w.desk.ask)
		},
		"bad format": func() (map[string]any, error) {
			return planstore.ProposePlanImport(contractCtx, w.rt, writer, "k", "r", bad, w.desk.ask)
		},
	} {
		_, err := call()
		wantIs(t, name, err, eventlog.ErrInvalid)
	}
	_, err := planstore.ProposePlanImport(contractCtx, w.rt, writer, "k", "r", req, nil)
	wantIs(t, "no question store", err, planstore.ErrAsk)
	_, err = planstore.ProposePlanImport(contractCtx, w.rt, writer, "k", "r", req, func(context.Context, planstore.Card) (string, error) { return "", errors.New("full") })
	wantIs(t, "a store that refuses", err, planstore.ErrAsk)
	if w.footprint() != before || w.desk.count() != 0 {
		t.Fatal("a refused import left something")
	}
	// A refused card leaves no plan, and the plan can still be imported.
	out, err := w.proposeImport("refused", req)
	must(t, err)
	w.desk.answer(w.questionOf(out), optionRefuse)
	w.decide()
	if got := w.decisionOf(out); got != "refused" || w.count(`SELECT count(*) FROM agentrt.plan_node`) != 0 {
		t.Fatalf("decided %q, with nodes", got)
	}
	w.importAndApply("second", req)
	if !w.hasNode(planRoot) {
		t.Fatal("the plan could not be imported after a refusal")
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestImportDecisionOnce: two controllers answering the same card at once must land one import, not two heads or a half-written tree.
func TestContractImportDecidedOnce(t *testing.T) {
	t.Parallel()
	w := emptyWorld(t)
	req := roundRequest(t)
	out, err := w.proposeImport("k", req)
	must(t, err)
	w.desk.answer(w.questionOf(out), optionApply)
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, derr := planstore.DecidePlanChanges(contractCtx, w.rt, w.desk.question, nil)
			if derr != nil {
				t.Error(derr)
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	if total != 1 || w.events("plan:R2", "legacy_imported") != 1 || w.events("plan:R2", "plan_change_decided") != 1 ||
		w.count(`SELECT count(*) FROM agentrt.plan_node`) != 1+len(req.Nodes) {
		t.Fatalf("two passes: %d decided, %d heads, %d decisions", total, w.events("plan:R2", "legacy_imported"), w.events("plan:R2", "plan_change_decided"))
	}
	w.decide()
	if got := w.desk.ackCount(w.questionOf(out)); got != 1 {
		t.Fatalf("acknowledged %d times", got)
	}
}

// Origin: 2026-11-18, TD-582: the "missing" case of store's TestImportDecisionBadRequest: a proposal whose request is no longer stored must be decided failed with the reason, written through the event the contract documents, and leave no plan; the wrong outcome is a pass that fails on it for ever and holds up every later card.
func TestContractImportWithoutItsRequestFails(t *testing.T) {
	t.Parallel()
	w := emptyWorld(t)
	missing := "sha256:" + strings.Repeat("c", 64)
	var proposal int64
	must(t, eventlog.InTx(contractCtx, w.rt, func(tx pgx.Tx) error {
		ev, err := eventlog.Append(contractCtx, tx, eventlog.Event{Stream: "plan:R2", Type: "plan_change_proposed", Actor: "controller",
			Body: map[string]any{"changes": []any{map[string]any{"op": "import", "artifact": missing}}, "reason": "test", "standing": false, "question_id": "q_x"}})
		proposal = ev.EventID
		return err
	}))
	w.desk.answer("q_x", optionApply)
	if n := w.decide(); n != 1 {
		t.Fatalf("decided %d", n)
	}
	out := map[string]any{"proposal_event_id": proposal}
	if got := w.decisionOf(out); got != "failed" || !strings.Contains(w.decisionError(out), "is not stored") {
		t.Fatalf("decided %q: %s", got, w.decisionError(out))
	}
	if w.count(`SELECT count(*) FROM agentrt.plan_node`) != 0 {
		t.Fatal("a plan appeared")
	}
}

// recorderLog is a recorder log as the lineage domain hands it over; only the
// parts the plan's side reads are real.
func recorderLog(work, body string) (planstore.RecorderLog, *recorderState) {
	st := &recorderState{}
	doc := []byte(`{"log":"` + body + `"}`)
	return planstore.RecorderLog{
		Work: work, Stream: "legacy:recorder:" + work, LogID: artifactID(doc), Doc: doc,
		CardBody: func(actor, key, reason string) string { return "log of " + work + " by " + actor + ": " + reason },
		Imported: func(context.Context, pgx.Tx) (int64, string, error) { return st.head, st.other, nil },
		TryImport: func(context.Context, pgx.Tx) error {
			st.tried++
			return st.tryErr
		},
	}, st
}

type recorderState struct {
	head   int64
	other  string
	tried  int
	tryErr error
}

// Origin: 2026-11-18, TD-582: the plan's half of store's TestRecorderImportThroughTheCard and TestRecorderImportRefused: a recorder log is proposed like a plan import, on its own stream, tried first and decided once; the wrong outcomes are a log imported twice, or an import proposed with no trial. The landing itself belongs to lineage (externalwork) and is not asserted here.
func TestContractRecorderImportProposal(t *testing.T) {
	t.Parallel()
	w := emptyWorld(t)
	log, st := recorderLog("td-1", "one")
	propose := func(key string, l planstore.RecorderLog, ask planstore.Ask) (map[string]any, error) {
		return planstore.ProposeRecorderImport(contractCtx, w.rt, writer, key, "retire the recorder", l, ask)
	}
	before := w.footprint()
	unprepared := log
	unprepared.TryImport = nil
	_, err := propose("k", unprepared, w.desk.ask)
	wantIs(t, "a log not prepared", err, eventlog.ErrInvalid)
	_, err = propose("", log, w.desk.ask)
	wantIs(t, "no key", err, eventlog.ErrInvalid)
	_, err = propose("k", log, nil)
	wantIs(t, "no question store", err, planstore.ErrAsk)
	st.tryErr = eventlog.Invalid("the chain breaks")
	_, err = propose("k", log, w.desk.ask)
	wantIs(t, "a trial that fails", err, eventlog.ErrInvalid)
	st.tryErr = nil
	if w.footprint() != before || w.desk.count() != 0 {
		t.Fatal("a refusal left something")
	}
	tried := st.tried
	out, err := propose("k", log, w.desk.ask)
	must(t, err)
	q := w.questionOf(out)
	c := w.desk.last()
	if st.tried != tried+1 || out["log_artifact_id"] != log.LogID || !strings.HasPrefix(c.Title, "Import recorder log td-1: ") ||
		len(c.Options) != 2 || c.Recommended != 1 || c.RecommendedWhy == "" || !strings.Contains(c.Body, "log of td-1") {
		t.Fatalf("answer %v, card %+v", out, c)
	}
	if n := w.events(log.Stream, "plan_change_proposed"); n != 1 {
		t.Fatalf("%d proposals on the log's stream", n)
	}
	if n := w.count(`SELECT count(*) FROM agentrt.artifact WHERE artifact_id = $1`, log.LogID); n != 1 {
		t.Fatal("the kept log was not stored")
	}
	// Waiting: the same log answers with its card, another log is refused.
	again, err := propose("k2", log, w.desk.ask)
	must(t, err)
	if again["question_id"] != q || w.desk.count() != 1 {
		t.Fatalf("asking again: %v", again)
	}
	other, _ := recorderLog("td-1", "two")
	_, err = propose("k3", other, w.desk.ask)
	wantIs(t, "another log waiting", err, eventlog.ErrInvalid)
	// The Owner refuses: decided on the log's stream, once.
	w.desk.answer(q, optionRefuse)
	if n := w.decide(); n != 1 || w.decisionOf(out) != "refused" || w.events(log.Stream, "plan_change_decided") != 1 {
		t.Fatalf("decided %d: %q", n, w.decisionOf(out))
	}
	// A log imported already answers so; another log of that work is refused.
	st.head = 77
	done, err := propose("k4", log, w.desk.ask)
	must(t, err)
	if done["decision"] != "applied" || done["imported"] != false || done["event_id"] == nil {
		t.Fatalf("an imported log: %v", done)
	}
	st.head, st.other = 0, "sha256:other"
	_, err = propose("k5", log, w.desk.ask)
	wantIs(t, "another log imported", err, eventlog.ErrInvalid)
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestArchImportBesideTheRound and TestArchImportRefusesALocalPath, through the card instead of ImportPlan: the table's rows arrive with the rules the request gives and each line's text as the row's first note, and a line a note may not hold refuses the whole import; the wrong outcomes are a table without its account of each row, or a path into one machine published in a note.
func TestContractArchImport(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	gates := `{"gates_passed":[{"test":"T05","spec_sha256":"` + strings.Repeat("ab", 32) + `"}],"env":"stg"}`
	req := archRequest(t, gates)
	out, err := w.proposeImport("arch", req)
	must(t, err)
	reqID, _ := out["request_artifact_id"].(string)
	w.applyCard(out)
	if rule := w.node(archRowA).rule; !containsAll(rule, `"test": "T05"`) {
		t.Fatalf("the row's rule: %s", rule)
	}
	if rule := w.node(archRowB).rule; rule != "{}" {
		t.Fatalf("a row given no rule has %s", rule)
	}
	var text, evidence string
	must(t, w.rt.QueryRow(contractCtx, `SELECT n.body_json->>'text', n.body_json->'evidence'->>0 FROM agentrt.event n
	  JOIN agentrt.event h ON n.cause_event_id = h.event_id
	  WHERE n.event_type = 'note' AND n.plan_node_id = $1 AND h.event_type = 'legacy_imported' AND h.event_stream_id = 'plan:ARCH'`, archRowB).Scan(&text, &evidence))
	if text != "两个环境都在用" || evidence != reqID {
		t.Fatalf("the first note %q with evidence %s; want the line's text and %s", text, evidence, reqID)
	}
	if got := w.node(stepLive).state; got != "active" {
		t.Fatalf("the round beside it moved: %s", got)
	}

	fresh := emptyWorld(t)
	bad, err := planimport.ParseArch("### W-31\n  - **架构对照（核对于 2026-10-19 01:20）**\n    - [~] 看板｜在 /Users/someone/agent-work/status 上跑\n",
		[]planimport.ArchRow{{Label: "board"}}, planimport.LedgerOptions{File: "WORK.md"})
	must(t, err)
	before := fresh.footprint()
	_, err = fresh.proposeImport("bad", bad)
	wantIs(t, "a line a note may not hold", err, eventlog.ErrInvalid)
	if !strings.Contains(err.Error(), "a local path in the note") || fresh.footprint() != before || fresh.desk.count() != 0 {
		t.Fatalf("the refusal: %v; cards %d", err, fresh.desk.count())
	}
}
