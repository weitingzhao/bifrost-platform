package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of every kind of runtime card
// (console/src/api/__fixtures__/question-cards.testdb.json): GET
// /api/v1/questions/{id} of one card of each kind, open, made by the
// runtime's own code on the real question store. Off unless asked for:
//
//	CONSOLE_QUESTION_FIXTURE_WRITE=1 go test ./internal/server/ -run TestOwnerButtonPaths
const consoleQuestionCardsFixture = "../../../console/src/api/__fixtures__/question-cards.testdb.json"

// buttonWorld is a runtime database and a question store, as the platform
// wires them, for the paths behind the Owner's buttons.
type buttonWorld struct {
	t    *testing.T
	conn *pgx.Conn
	rt   *questionRuntime
}

func (b *buttonWorld) tx(fn func(pgx.Tx) error) {
	b.t.Helper()
	if err := store.InTx(context.Background(), b.conn, fn); err != nil {
		b.t.Fatal(err)
	}
}

// legacy runs a statement under an import event, as a fixture may.
func (b *buttonWorld) legacy(stmt string) {
	b.t.Helper()
	b.tx(func(tx pgx.Tx) error {
		if _, err := store.Append(context.Background(), tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), stmt)
		return err
	})
}

// staleMission makes mission m, 30 hours old on a two-hour budget, with one
// ready task, and returns the task.
func (b *buttonWorld) staleMission(m string) string {
	b.t.Helper()
	ctx := context.Background()
	task := store.NewTaskID()
	b.tx(func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: m, Title: "goal " + m, Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess}}); err != nil {
			return err
		}
		if err := store.MoveMission(ctx, tx, "controller", m, "active", "test"); err != nil {
			return err
		}
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# task\n"))
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: task, MissionID: m, OffPlanReason: "a goal", Role: "implement",
			Effect: "branch", Repo: "bifrost-platform", Class: "go", Title: "a task of " + m, SpecArtifact: spec}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", task)
	})
	b.legacy(`UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '30 hours',
	  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = '` + m + `'`)
	return task
}

// answer answers a card as the Owner's Console does: POST
// /api/v1/questions/{id}/answer from the session owner-console.
func (b *buttonWorld) answer(id, option, text string) {
	b.t.Helper()
	body := map[string]any{"channel": "console", "option": option}
	if text != "" {
		body["text"] = text
	}
	raw, _ := json.Marshal(body)
	if res := b.rt.svc.Answer(context.Background(), "owner-console", "admin", "admin", id, raw); res.Code != 200 {
		b.t.Fatalf("answer %s with %s: %d %v", id, option, res.Code, res.Body)
	}
}

func (b *buttonWorld) state(table, id string) string {
	b.t.Helper()
	var s string
	if err := b.conn.QueryRow(context.Background(), fmt.Sprintf(`SELECT state FROM agentrt.%[1]s WHERE %[1]s_id = $1`, table), id).Scan(&s); err != nil {
		b.t.Fatal(err)
	}
	return s
}

func (b *buttonWorld) decision(question string) string {
	b.t.Helper()
	var d string
	if err := b.conn.QueryRow(context.Background(), `SELECT body_json->>'decision' FROM agentrt.event WHERE event_type = 'plan_change_decided'
	  AND body_json->>'question_id' = $1`, question).Scan(&d); err != nil {
		b.t.Fatal(err)
	}
	return d
}

// recorderLog seals events the way the recorder's events.py does.
func recorderLog(t *testing.T, work string) recorderimport.Request {
	t.Helper()
	var lines []string
	prev := ""
	for i, e := range []map[string]any{{"type": "work_opened", "work_id": work, "title": "a work"}, {"type": "work_closed", "work_id": work}} {
		e["v"], e["seq"], e["prev_sha256"], e["by"], e["at"] = 2, i+1, prev, "coordinator", 1791952517.25+float64(i)
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		s := sha256.Sum256(raw)
		prev = hex.EncodeToString(s[:])
		lines = append(lines, prev+" "+string(raw))
	}
	all := sha256.Sum256([]byte(strings.Join(lines, "\n") + "\n"))
	return recorderimport.Request{Format: recorderimport.Format, Lines: lines,
		Source: recorderimport.Source{WorkID: work, File: work + "/events.jsonl", SHA256: hex.EncodeToString(all[:])}}
}

// TD-393 (the Owner 2026-10-20 「走查不再用「需要本人」，按钮的路径用 Console 测
// 试」): every button the Owner has on a runtime card, from the answer his
// Console sends to what the runtime does with it, on the real question store
// and a real database, each card made by the runtime's own code:
//   - a blocked task's card: a gives it one more round (its budget raised, the
//     task ready again), b does not do the task, c closes the mission;
//   - a plan change's card: a applies it, b refuses it;
//   - a plan import's card and a recorder import's: a imports, b refuses;
//   - a mission's card: a starts it, b does not, "none of these" with his words
//     does not either;
//   - a planner's question: any option is taken and acknowledged (nothing
//     reads it; the work goes on with its recommendation).
//
// The Console's side, that each button sends that answer, is
// console/src/pages/__tests__/ownerButtons.test.tsx, on the fixture this
// writes.
func TestOwnerButtonPaths(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	b := &buttonWorld{t: t, conn: db.Connect(t, db.Runtime), rt: newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)}
	if _, err := b.conn.Exec(ctx, agentrttest.DevBudgetRule); err != nil {
		t.Fatal(err)
	}
	// One question store a group of cards: the store takes six cards an
	// hour from the runtime.
	var ask store.Ask
	var question func(context.Context, string) (store.Question, bool)
	var ack func(context.Context, string) error
	group := func() {
		b.rt = newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
		ask, question, ack = b.rt.agentRuntimeAsk(), b.rt.agentRuntimeQuestion(), b.rt.agentRuntimeAck()
	}
	group()
	thread := store.Writer{Actor: "thread:buttons", Token: "t"}
	fixture := map[string]any{}
	keep := func(kind, id string) {
		res := b.rt.svc.Get(ctx, "owner-console", "admin", "admin", id)
		if res.Code != 200 {
			t.Fatalf("GET %s: %d %v", id, res.Code, res.Body)
		}
		fixture[kind] = map[string]any{"status": res.Code, "body": res.Body}
	}

	// Blocked cards: a, b and c, each on its own stale mission.
	tasks := map[string]string{"a": b.staleMission("M-3"), "b": b.staleMission("M-4"), "c": b.staleMission("M-5")}
	if n, err := store.BlockOverBudget(ctx, b.conn); err != nil || n != 3 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	if n, err := store.AskBlocked(ctx, b.conn, ask); err != nil || n != 3 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	cards, err := b.rt.agentRuntimeOpenCards()(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		for option, task := range tasks {
			if c.TaskID == task {
				if option == "c" {
					keep("blocked", c.ID)
				}
				b.answer(c.ID, option, "")
			}
		}
	}
	if n, err := store.DecideBlocked(ctx, b.conn, question, ack); err != nil || n != 3 {
		t.Fatalf("blocked cards decided %d (%v)", n, err)
	}
	if got := b.state("task", tasks["a"]); got != "ready" {
		t.Errorf("a (one more round): the task is %s", got)
	}
	if got := b.state("task", tasks["b"]); got != "cancelled" {
		t.Errorf("b (do not do it): the task is %s", got)
	}
	if got := b.state("mission", "M-5"); got != "stopped" {
		t.Errorf("c (close the mission): M-5 is %s", got)
	}

	group()
	// Plan changes: a applies, b refuses. Two steps of a plan, bound to M-3,
	// each proposed to move to M-4.
	b.tx(func(tx pgx.Tx) error {
		for _, n := range []store.PlanNode{
			{ID: "R9", Kind: "round", Label: "9", Title: "round", Root: "R9"},
			{ID: "R9:1", Parent: "R9", Kind: "item", Label: "1", Title: "item", Root: "R9"},
			{ID: "R9:1-1", Parent: "R9:1", Kind: "step", Label: "1-1", Title: "step", MissionID: "M-3", StepKind: "agent", Root: "R9"},
			{ID: "R9:1-2", Parent: "R9:1", Kind: "step", Label: "1-2", Title: "step", MissionID: "M-3", StepKind: "agent", Root: "R9", Ordinal: 1},
		} {
			if _, err := store.CreatePlanNode(ctx, tx, "controller", n); err != nil {
				return err
			}
		}
		return nil
	})
	planCards := map[string]string{}
	for option, step := range map[string]string{"a": "R9:1-1", "b": "R9:1-2"} {
		out, err := store.ProposePlanChange(ctx, b.conn, thread, "rebind-"+option, "R9", "move "+step,
			[]store.PlanChange{{Op: "rebind_step", Node: step, MissionID: "M-4"}}, ask)
		if err != nil {
			t.Fatal(err)
		}
		planCards[option], _ = out["question_id"].(string)
		if option == "a" {
			keep("plan_change", planCards[option])
		}
		b.answer(planCards[option], option, "")
	}

	// Imports: a plan import (a) and its refusal (b); two recorder logs.
	text, err := os.ReadFile("../agentrt/planimport/testdata/ledger.md")
	if err != nil {
		t.Fatal(err)
	}
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := store.ProposePlanImport(ctx, b.conn, thread, "import", "round 2 into the runtime", req, ask)
	if err != nil {
		t.Fatal(err)
	}
	importCard, _ := out["question_id"].(string)
	keep("plan_import", importCard)
	b.answer(importCard, "b", "")
	recorderCards := map[string]string{}
	for option, work := range map[string]string{"a": "td-901", "b": "td-902"} {
		out, err := store.ProposeRecorderImport(ctx, b.conn, thread, "recorder-"+option, "retire the recorder", recorderLog(t, work), ask)
		if err != nil {
			t.Fatal(err)
		}
		recorderCards[option], _ = out["question_id"].(string)
		if option == "a" {
			keep("recorder_import", recorderCards[option])
		}
		b.answer(recorderCards[option], option, "")
	}
	if n, err := store.DecidePlanChanges(ctx, b.conn, question, ack); err != nil || n != 5 {
		t.Fatalf("proposals decided %d (%v)", n, err)
	}
	for card, want := range map[string]string{planCards["a"]: "applied", planCards["b"]: "refused", importCard: "refused",
		recorderCards["a"]: "applied", recorderCards["b"]: "refused"} {
		if got := b.decision(card); got != want {
			t.Errorf("proposal of %s: %s; want %s", card, got, want)
		}
	}
	var bound string
	if err = b.conn.QueryRow(ctx, `SELECT mission_id FROM agentrt.plan_node WHERE plan_node_id = 'R9:1-1'`).Scan(&bound); err != nil || bound != "M-4" {
		t.Errorf("the applied change: R9:1-1 is bound to %s (%v)", bound, err)
	}

	group()
	// Missions outside the standing envelope: a starts, b and "none of
	// these" do not.
	missions := map[string]string{}
	for _, option := range []string{"a", "b", "none"} {
		out, err := store.ProposeMission(ctx, b.conn, thread, "mission-"+option,
			store.MissionProposal{Title: "a mission " + option, Goal: "g", Success: []string{"done"}}, ask)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := out["mission_id"].(string)
		q, _ := out["question_id"].(string)
		missions[option] = id
		words := ""
		if option == "none" {
			words = "not this week"
		}
		if option == "a" {
			keep("mission", q)
		}
		b.answer(q, option, words)
	}
	if n, err := store.DecideMissions(ctx, b.conn, question, ack); err != nil || n != 3 {
		t.Fatalf("missions decided %d (%v)", n, err)
	}
	for option, want := range map[string]string{"a": "active", "b": "stopped", "none": "stopped"} {
		if got := b.state("mission", missions[option]); got != want {
			t.Errorf("mission card answered %s: %s; want %s", option, got, want)
		}
	}

	// A planner's question: asked as planresult.go asks it.
	planner, err := ask(ctx, store.Card{Key: strings.Repeat("pq", 16), Title: "Which page first?", Body: "The plan starts with one of two pages.",
		Options: []string{"The work page", "The chief page"}, Recommended: 1, Meanwhile: "the plan starts with the work page",
		OwnerDuty: "goal", TaskID: tasks["a"]})
	if err != nil {
		t.Fatal(err)
	}
	keep("plan_question", planner)
	b.answer(planner, "b", "")
	if err = ack(ctx, planner); err != nil {
		t.Fatalf("the planner's answer acknowledged: %v", err)
	}

	if os.Getenv("CONSOLE_QUESTION_FIXTURE_WRITE") == "1" {
		raw, err := json.MarshalIndent(fixture, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(consoleQuestionCardsFixture, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
