package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gaterun"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// The architecture table's gates run by the runtime (TD-397,
// CUT-td397-gate-runs-2026-10-20.md).

var (
	refA = map[string]any{"delivery_run": "bifrost-deliver-platform-1", "images": map[string]any{"platform-api": "sha256:aa"}}
	refB = map[string]any{"delivery_run": "bifrost-deliver-platform-2", "images": map[string]any{"platform-api": "sha256:bb"}}
)

const commitC = "4c1d3a5e7f9b2d4f6a8c0e2a4c6e8a0c2e4a6c8e"

// gateOf is a gate on one package, its spec and sha256.
func gateOf(t *testing.T, id string, tests ...string) (gaterun.Gate, []byte, string) {
	t.Helper()
	g := gaterun.Gate{ID: id, Text: "gate " + id, Commands: []gaterun.Command{{Pkg: "./internal/agentrt/plan/", Tests: tests}}}
	body, sum, err := g.Spec()
	ok(t, err)
	return g, body, sum
}

// gateWorld is a kernel world with the standing envelope the gates run in
// (the deterministic executor on h1), each gate's spec stored, and the
// architecture rows with these rules.
func gateWorld(t *testing.T, specs [][]byte, rows ...[2]string) *world {
	t.Helper()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, ruleText("standing-envelope:dev", 1, rules.Initial, since, until, `tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:deterministic]
hosts: [h1, h2]
`))))
	for _, b := range specs {
		w.artifact(t, "task_spec", string(b))
	}
	w.arch(t, rows...)
	return w
}

func gateRule(env string, gates ...[2]string) string {
	var list []string
	for _, g := range gates {
		list = append(list, `{"test":"`+g[0]+`","spec_sha256":"`+g[1]+`"}`)
	}
	return `{"gates_passed":[` + strings.Join(list, ",") + `],"env":"` + env + `"}`
}

func (w *world) runGates(t *testing.T, commit string, ref map[string]any) int {
	t.Helper()
	n, err := store.RunGates(ctx, w.rt, "dev", "bifrost-platform", deployedIs(commit, ref))
	ok(t, err)
	return n
}

func (w *world) deploy(t *testing.T, commit string, ref map[string]any) {
	t.Helper()
	_, err := store.SeeDeploy(ctx, w.rt, "dev", deployedIs(commit, ref))
	ok(t, err)
}

// gateTasks is the gate tasks of the batches of a commit: task, test, state.
func (w *world) gateTasks(t *testing.T, commit string) [][3]string {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT g->>'task', g->>'test', t.state FROM agentrt.mission m,
	  jsonb_array_elements(m.scope_json->'gate_run'->'gates') g JOIN agentrt.task t ON t.task_id = g->>'task'
	  WHERE m.scope_json->'gate_run'->>'commit' = $1 ORDER BY g->>'test', t.created_at`, commit)
	ok(t, err)
	var out [][3]string
	for rows.Next() {
		var r [3]string
		ok(t, rows.Scan(&r[0], &r[1], &r[2]))
		out = append(out, r)
	}
	ok(t, rows.Err())
	return out
}

// verifyJSON is a verify.json of a gate's commands: each exits code, its tail
// ends with tail.
func gateVerifyJSON(g gaterun.Gate, pass bool, code int, tail string) string {
	cmds := []any{}
	for _, c := range g.Commands {
		cmds = append(cmds, map[string]any{"argv": c.Argv(), "exit_code": code, "seconds": 1, "tail": tail})
	}
	b, _ := json.Marshal(map[string]any{"pass": pass, "commands": cmds})
	return string(b)
}

// runGate takes the gate task on h1 with the deterministic executor, hands
// in report as its verify.json with conclusion, and uses the result.
func (w *world) runGate(t *testing.T, task, conclusion, report string) string {
	t.Helper()
	c := w.claim(t, "h1", store.LeaseTTL, deterministic)
	if c == nil || c.TaskID != task || c.Role != "verify" {
		t.Fatalf("the gate task was not leased to the deterministic executor: %+v", c)
	}
	f := fence(c, "h1")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	if conclusion != "done" {
		exit = 1
	}
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", conclusion)
	ok(t, err)
	calls := 0
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "verify", report)}, heads(nil, &calls))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	did, err := store.Conclude(ctx, w.rt, task)
	ok(t, err)
	return did
}

// TD-397: a row's gate runs on what the environment runs, once per
// deployment, and ticks the row; two rows sharing a gate share its run; a
// gate added later gets a batch of its own; a new deployment takes the tick
// away and cancels the old deployment's tasks still waiting.
func TestGatesRunOnTheDeployedCommit(t *testing.T) {
	t.Parallel()
	g, spec, sum := gateOf(t, "T40", "TestGrammarIsClosed")
	_, spec2, sum2 := gateOf(t, "T41", "TestGateUsesTheExactTestID")
	w := gateWorld(t, [][]byte{spec, spec2}, [2]string{"ARCH:a", gateRule("dev", [2]string{"T40", sum})},
		[2]string{"ARCH:b", gateRule("dev", [2]string{"T40", sum})}, [2]string{"ARCH:other", gateRule("prod", [2]string{"T40", sum})})
	if n := w.runGates(t, commitA, refA); n != 0 {
		t.Fatalf("dispatched %d with nothing recorded", n)
	}
	w.deploy(t, commitA, refA)
	if n := w.runGates(t, commitB, refA); n != 0 {
		t.Fatalf("dispatched %d when the live read is not what was recorded", n)
	}
	if n := w.runGates(t, commitA, refA); n != 1 {
		t.Fatalf("dispatched %d; want one task for the gate both rows name", n)
	}
	if n := w.runGates(t, commitA, refA); n != 0 {
		t.Fatalf("a second pass dispatched %d", n)
	}
	tasks := w.gateTasks(t, commitA)
	if len(tasks) != 1 || tasks[0][1] != "T40" || tasks[0][2] != "ready" {
		t.Fatalf("tasks %v", tasks)
	}
	var state, title, base, specID, class string
	var subject *string
	ok(t, w.rt.QueryRow(ctx, `SELECT m.state, t.title, t.base_commit, t.spec_artifact_id, t.task_class, t.subject_task_id FROM agentrt.task t
	  JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1`, tasks[0][0]).Scan(&state, &title, &base, &specID, &class, &subject))
	if state != "active" || base != commitA || specID != "sha256:"+sum || class != store.GateClass || subject != nil || title != "Gate T40 on dev at "+commitA[:12] {
		t.Fatalf("batch %s, task %q %s %s %s %v", state, title, base, specID, class, subject)
	}
	if did := w.runGate(t, tasks[0][0], "done", gateVerifyJSON(g, true, 0, "gate verdict: PASS ./internal/agentrt/plan/: 1 tests ran and passed")); did != "accepted" {
		t.Fatalf("did %q", did)
	}
	rows := w.archRows(t)
	wantTick(t, rows["ARCH:a"], "true", "T40 passed on dev at 2333e00753f9")
	wantTick(t, rows["ARCH:b"], "true", "T40 passed on dev")
	var verifier *string
	var verdict, checkRef, env, commit, specSum string
	ok(t, w.rt.QueryRow(ctx, `SELECT verdict, check_ref, env, subject_commit, spec_sha256, verifier_attempt_id FROM agentrt.verification
	  WHERE task_id = $1 AND method = 'gate'`, tasks[0][0]).Scan(&verdict, &checkRef, &env, &commit, &specSum, &verifier))
	if verdict != "pass" || checkRef != "T40" || env != "dev" || commit != commitA || specSum != sum || verifier != nil {
		t.Fatalf("verification %s %s %s %s %s %v", verdict, checkRef, env, commit, specSum, verifier)
	}
	if n, err := store.EndGates(ctx, w.rt, "dev"); err != nil || n != 1 {
		t.Fatalf("ended %d (%v); want the batch closed", n, err)
	}
	if s := w.state(t, "mission", "M-3"); s != "done" {
		t.Fatalf("the batch is %s", s)
	}

	// A new deployment: the tick goes; its batch; a gate added later.
	w.deploy(t, commitB, refB)
	wantTick(t, w.archRows(t)["ARCH:a"], "false", "T40 (not dispatched yet): no pass on dev at 0b9e1f3c7a1d")
	if n := w.runGates(t, commitB, refB); n != 1 {
		t.Fatalf("dispatched %d for the new deployment", n)
	}
	wantTick(t, w.archRows(t)["ARCH:a"], "false", "T40 (not run yet)")
	w.setArchRule(t, "ARCH:a", gateRule("dev", [2]string{"T40", sum}, [2]string{"T41", sum2}))
	if n := w.runGates(t, commitB, refB); n != 1 {
		t.Fatalf("dispatched %d for the gate added later", n)
	}
	if got := w.gateTasks(t, commitB); len(got) != 2 || got[0][1] != "T40" || got[1][1] != "T41" {
		t.Fatalf("tasks of the new deployment %v", got)
	}
	// Another deployment: both waiting tasks are cancelled, both batches stop.
	w.deploy(t, commitC, refA)
	if n, err := store.EndGates(ctx, w.rt, "dev"); err != nil || n != 4 {
		t.Fatalf("ended %d (%v); want two tasks cancelled and two batches closed", n, err)
	}
	for _, r := range w.gateTasks(t, commitB) {
		if r[2] != "cancelled" || !strings.HasPrefix(w.lastStateReason(t, r[0]), "the gate could not run: what dev runs is now 4c1d3a5e7f9b") {
			t.Fatalf("task %v: %q", r, w.lastStateReason(t, r[0]))
		}
	}
	// The rule stops at its first gate that has not passed: the reason names it.
	wantTick(t, w.archRows(t)["ARCH:a"], "false", "T40 (not dispatched yet): no pass on dev at 4c1d3a5e7f9b")
}

func (w *world) lastStateReason(t *testing.T, task string) string {
	t.Helper()
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->>'reason', '') FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  ORDER BY event_id DESC LIMIT 1`, task).Scan(&reason))
	return reason
}

// A gate passes only on a report of its own commands, each exiting 0, that
// says pass, of an attempt that concluded done; anything else records a
// failure, with the runner's verdict line on a note the table quotes.
func TestGateVerdicts(t *testing.T) {
	t.Parallel()
	g, spec, sum := gateOf(t, "T40", "TestGrammarIsClosed")
	other := g
	other.Commands = []gaterun.Command{{Pkg: "./internal/agentrt/rules/", Tests: []string{"TestTightens"}}}
	withheld := strings.Replace(gateVerifyJSON(g, true, 0, ""), `"argv":["go"`, `"argv":["withheld"],"x":["go"`, 1)
	for name, c := range map[string]struct {
		conclusion, report, verdict, reason string
	}{
		"its own commands pass": {"done", gateVerifyJSON(g, true, 0, "gate verdict: PASS p: 1 tests ran and passed"), "pass", "T40 passed"},
		"a failing test": {"failed", gateVerifyJSON(g, false, 1, "--- FAIL\ngate verdict: FAIL ./internal/agentrt/plan/: TestGrammarIsClosed it failed\n"), "fail",
			"T40 (failed: FAIL ./internal/agentrt/plan/: TestGrammarIsClosed it failed)"},
		"other commands":         {"done", gateVerifyJSON(other, true, 0, ""), "fail", "T40 (failed: command 1 is not its spec's"},
		"a trace withheld":       {"done", withheld, "fail", "T40 (failed: command 1 is not its spec's (or its trace was withheld))"},
		"pass but not concluded": {"failed", gateVerifyJSON(g, true, 0, ""), "fail", "T40 (failed: its check did not pass"},
		"not verify.json":        {"done", "# a report\n", "fail", "T40 (failed: its verify.json does not read"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := gateWorld(t, [][]byte{spec}, [2]string{"ARCH:a", gateRule("dev", [2]string{"T40", sum})})
			w.deploy(t, commitA, refA)
			if n := w.runGates(t, commitA, refA); n != 1 {
				t.Fatalf("dispatched %d", n)
			}
			task := w.gateTasks(t, commitA)[0][0]
			if did := w.runGate(t, task, c.conclusion, c.report); did != "accepted" {
				t.Fatalf("did %q", did)
			}
			var verdict string
			ok(t, w.rt.QueryRow(ctx, `SELECT verdict FROM agentrt.verification WHERE task_id = $1 AND method = 'gate'`, task).Scan(&verdict))
			if verdict != c.verdict {
				t.Fatalf("verdict %s; want %s", verdict, c.verdict)
			}
			tick := "true"
			if c.verdict != "pass" {
				tick = "false"
			}
			wantTick(t, w.archRows(t)["ARCH:a"], tick, c.reason)
			if did, err := store.Conclude(ctx, w.rt, task); err != nil || did != "" {
				t.Fatalf("a second use did %q (%v)", did, err)
			}
		})
	}
}

// A gate that cannot run ends without a card: every executor withdrawn
// (TD-381; neither TD-395's pass nor the budget pass blocks it), the batch
// past its deadline, its budget spent; a member a claim holds is left to the
// next pass; the batch closes stopped with a note of what became of it.
func TestGatesThatCannotRunEnd(t *testing.T) {
	t.Parallel()
	_, spec, sum := gateOf(t, "T40", "TestGrammarIsClosed")
	_, spec2, sum2 := gateOf(t, "T41", "TestGateUsesTheExactTestID")
	w := gateWorld(t, [][]byte{spec, spec2}, [2]string{"ARCH:a", gateRule("dev", [2]string{"T40", sum}, [2]string{"T41", sum2})})
	w.deploy(t, commitA, refA)
	if n := w.runGates(t, commitA, refA); n != 2 {
		t.Fatalf("dispatched %d", n)
	}
	tasks := w.gateTasks(t, commitA)
	t40, t41 := tasks[0][0], tasks[1][0]
	for i := 0; i < 3; i++ {
		c := w.claim(t, "h1", store.LeaseTTL, deterministic)
		if c == nil {
			t.Fatalf("round %d: nothing leased", i)
		}
		w.fail(t, c, "h1")
		w.ago(t, c.TaskID, 10*time.Minute)
	}
	for _, pass := range []func() (int, error){func() (int, error) { return store.BlockWithdrawn(ctx, w.rt) },
		func() (int, error) { return store.BlockOverBudget(ctx, w.rt) }} {
		if n, err := pass(); err != nil || n != 0 {
			t.Fatalf("a pass blocked %d gate tasks (%v)", n, err)
		}
	}
	var failedOn string
	ok(t, w.rt.QueryRow(ctx, `SELECT task_id FROM agentrt.attempt WHERE state = 'failed' GROUP BY task_id HAVING count(*) = 3`).Scan(&failedOn))
	held, err := w.db.Pool(t, w.db.Runtime).Begin(ctx)
	ok(t, err)
	_, err = held.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR UPDATE`, failedOn)
	ok(t, err)
	if n, err := store.EndGates(ctx, w.rt, "dev"); err != nil || n != 0 {
		t.Fatalf("ended %d (%v) while a member is held", n, err)
	}
	ok(t, held.Rollback(ctx))
	if n, err := store.EndGates(ctx, w.rt, "dev"); err != nil || n != 1 {
		t.Fatalf("ended %d (%v); want the withdrawn gate cancelled", n, err)
	}
	if r := w.lastStateReason(t, failedOn); !strings.HasPrefix(r, "the gate could not run: exec:deterministic failed attempts") {
		t.Fatalf("the withdrawn gate: %q", r)
	}
	// The other is past the batch's deadline.
	w.legacy(t, `UPDATE agentrt.mission SET created_at = created_at - interval '7 hours' WHERE scope_json ? 'gate_run'`)
	if n, err := store.EndGates(ctx, w.rt, "dev"); err != nil || n != 2 {
		t.Fatalf("ended %d (%v); want the other cancelled and the batch closed", n, err)
	}
	rest := t41
	if failedOn == t41 {
		rest = t40
	}
	if r := w.lastStateReason(t, rest); r != "the gate could not run: its batch is past its deadline (6 hours)" {
		t.Fatalf("the other gate: %q", r)
	}
	var note string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_type = 'note' AND mission_id = 'M-3' ORDER BY event_id DESC LIMIT 1`).
		Scan(&note))
	if w.state(t, "mission", "M-3") != "stopped" || note != "the gates of dev at 2333e00753f9 ended: 0 passed, 0 failed, 2 could not run" {
		t.Fatalf("the batch is %s: %q", w.state(t, "mission", "M-3"), note)
	}
	if n, err := store.AskBlocked(ctx, w.rt, (&asker{}).ask); err != nil || n != 0 {
		t.Fatalf("asked %d cards (%v)", n, err)
	}
}

// A spent budget ends the batch's waiting gates too.
func TestGateBudgetSpentEnds(t *testing.T) {
	t.Parallel()
	_, spec, sum := gateOf(t, "T40", "TestGrammarIsClosed")
	w := gateWorld(t, [][]byte{spec}, [2]string{"ARCH:a", gateRule("dev", [2]string{"T40", sum})})
	w.deploy(t, commitA, refA)
	w.runGates(t, commitA, refA)
	task := w.gateTasks(t, commitA)[0][0]
	c := w.claim(t, "h1", store.LeaseTTL, deterministic)
	w.fail(t, c, "h1")
	w.legacy(t, `UPDATE agentrt.mission SET budget_json = '{"max_attempts": 1, "max_wall_hours": 6}' WHERE scope_json ? 'gate_run'`)
	if n, err := store.EndGates(ctx, w.rt, "dev"); err != nil || n != 2 {
		t.Fatalf("ended %d (%v)", n, err)
	}
	if r := w.lastStateReason(t, task); !strings.HasPrefix(r, "the gate could not run: ") || !strings.Contains(r, "attempts") {
		t.Fatalf("%q", r)
	}
}

// A batch is the controller's alone: no proposed task joins it, no plan
// change moves its member or binds a step to it, a member blocked by any
// path gets no card, and a done rule naming a spec that is not a stored gate
// spec is refused (review td397-2 A3, td397-3 B-a).
func TestGateBatchIsTheControllers(t *testing.T) {
	t.Parallel()
	_, spec, sum := gateOf(t, "T40", "TestGrammarIsClosed")
	w := gateWorld(t, [][]byte{spec}, [2]string{"ARCH:a", gateRule("dev", [2]string{"T40", sum})}, [2]string{"ARCH:b", ""})
	w.deploy(t, commitA, refA)
	w.runGates(t, commitA, refA)
	task := w.gateTasks(t, commitA)[0][0]
	_, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k1", "M-3", []store.TaskProposal{{Ref: "x", OffPlanReason: "x", Role: "implement",
		Class: "go", Title: "x", SpecArtifact: w.spec, Effect: "none"}})
	wantErr(t, err, "is a batch of gates and takes no proposed tasks")
	a := &asker{}
	for name, c := range map[string]store.PlanChange{
		"move its member":   {Op: "move_task", Task: task, Node: s1},
		"bind a step to it": {Op: "rebind_step", Node: s1, MissionID: "M-3"},
		"add a step to it":  {Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "x", MissionID: "M-3"},
	} {
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), name, root, "test", []store.PlanChange{c}, a.ask)
		if err == nil || !strings.Contains(err.Error(), "gate") {
			t.Errorf("%s: %v", name, err)
		}
	}
	w.artifact(t, "task_spec", "# not a gate\n")
	unstored := strings.Repeat("ab", 32)
	for name, rule := range map[string]string{"a spec not stored": gateRule("dev", [2]string{"T41", unstored}),
		"a spec that is no gate's": gateRule("dev", [2]string{"T41", sha256Hex("# not a gate\n")})} {
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), name, "ARCH", "test",
			[]store.PlanChange{{Op: "set_done_rule", Node: "ARCH:b", Rule: json.RawMessage(rule)}}, a.ask)
		wantErr(t, err, "gate T41")
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", task, "blocked") }))
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 || len(a.cards) != 0 {
		t.Fatalf("asked %d (%v) for a blocked gate", n, err)
	}
}

// The plan-change card recommends applying only the reviewed catalog's
// first gates for rows with no rule, in this environment; a rule answering
// it applies it only while that still holds (CUT §7② A).
func TestGatePlanChangeRecommendation(t *testing.T) {
	t.Parallel()
	row := gaterun.Catalog[0]
	var specs [][]byte
	for _, g := range row.Gates {
		body, _, err := g.Spec()
		ok(t, err)
		specs = append(specs, body)
	}
	_, otherSpec, otherSum := gateOf(t, row.Gates[0].ID, "TestGrammarIsClosed")
	specs = append(specs, otherSpec)
	label := "ARCH:" + row.Label
	w := gateWorld(t, specs, [2]string{label, ""}, [2]string{"ARCH:" + gaterun.Catalog[1].Label, ""})
	want, err := row.Rule("dev")
	ok(t, err)
	prodRule, err := row.Rule("prod")
	ok(t, err)
	a := &asker{}
	propose := func(key string, changes ...store.PlanChange) store.Card {
		t.Helper()
		n := len(a.cards)
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, "ARCH", "the catalog's gates", changes, a.ask)
		ok(t, err)
		if len(a.cards) != n+1 {
			t.Fatalf("%s: no card", key)
		}
		return a.cards[n]
	}
	set := func(node string, rule []byte) store.PlanChange {
		return store.PlanChange{Op: "set_done_rule", Node: node, Rule: json.RawMessage(rule)}
	}
	if c := propose("exact", set(label, want)); c.Recommended != 1 || !strings.Contains(c.RecommendedWhy, "the reviewed catalog's for dev") {
		t.Fatalf("the catalog's rule: %+v", c)
	}
	for key, changes := range map[string][]store.PlanChange{
		"another env":     {set(label, prodRule)},
		"another row":     {set("ARCH:"+gaterun.Catalog[1].Label, want)},
		"the row twice":   {set(label, want), set(label, want)},
		"mixed":           {set(label, want), {Op: "retitle", Node: label, Title: "x"}},
		"not the catalog": {set(label, []byte(gateRule("dev", [2]string{row.Gates[0].ID, otherSum})))},
	} {
		if c := propose(key, changes...); c.Recommended != 0 {
			t.Errorf("%s: recommended %d (%s)", key, c.Recommended, c.RecommendedWhy)
		}
	}
	// The rule answers the recommended card; a row given a rule meanwhile is
	// not overwritten: the change fails.
	w.setArchRule(t, label, string(prodRule))
	byRule := func(_ context.Context, id string) (store.Question, bool) {
		if id != "q_a" { // the first card, the catalog's rule
			return store.Question{ID: id, Status: "open"}, true
		}
		return store.Question{ID: id, Status: store.QuestionAnswered, Option: store.CardApplyKey, AnsweredBy: "rule card-auto-answer v3",
			AnsweredAt: "2026-10-21T00:00:00Z", Channel: "rule"}, true
	}
	if _, err := store.DecidePlanChanges(ctx, w.rt, byRule, a.ack); err != nil {
		t.Fatal(err)
	}
	var decision, why string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'decision', COALESCE(body_json->>'error', '') FROM agentrt.event WHERE event_type = 'plan_change_decided'
	  AND event_stream_id = 'plan:ARCH' ORDER BY event_id LIMIT 1`).Scan(&decision, &why))
	if decision != "failed" || !strings.Contains(why, "no longer the catalog's first gates") {
		t.Fatalf("decided %s: %s", decision, why)
	}
	if rule := w.ruleOf(t, label); rule != string(prodRule) {
		t.Fatalf("the row's rule became %s", rule)
	}
}

// setArchRule sets a row's rule as a plan change writes it.
func (w *world) setArchRule(t *testing.T, node, rule string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("ARCH"), Type: "plan_changed", Actor: "controller",
			Body: map[string]any{"change": "set_done_rule"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, node, rule)
		return err
	}))
}

func (w *world) ruleOf(t *testing.T, node string) string {
	t.Helper()
	var doc string
	ok(t, w.rt.QueryRow(ctx, `SELECT done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = $1`, node).Scan(&doc))
	v, err := canon.Marshal([]byte(doc))
	ok(t, err)
	return string(v)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// 0007 (TD-397): a verify task with no subject goes in only as a gate task
// the controller makes: class gate, effect none, off the plan. An ordinary
// verify or review still needs its subject.
func TestGateTaskConstraint(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	try := func(actor string, task store.Task) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			task.ID, task.MissionID, task.Title, task.SpecArtifact = store.NewTaskID(), "M-1", "x", w.spec
			if task.PlanNode == "" {
				task.OffPlanReason = "x"
			}
			_, err := store.ProposeTask(ctx, tx, actor, task)
			return err
		})
	}
	gate := store.Task{Role: "verify", Class: store.GateClass, Effect: "none", Repo: "bifrost-platform", BaseCommit: commitA}
	ok(t, try("controller", gate))
	for name, c := range map[string]struct {
		actor string
		task  store.Task
	}{
		"an ordinary verify":  {"controller", store.Task{Role: "verify", Class: "go", Effect: "none"}},
		"a review":            {"controller", store.Task{Role: "review", Class: store.GateClass, Effect: "none"}},
		"a gate with effect":  {"controller", store.Task{Role: "verify", Class: store.GateClass, Effect: "branch"}},
		"a gate on the plan":  {"controller", store.Task{Role: "verify", Class: store.GateClass, Effect: "none", PlanNode: s1}},
		"a gate by the Owner": {"owner", gate},
	} {
		if err := try(c.actor, c.task); err == nil || !strings.Contains(err.Error(), "23514") {
			t.Errorf("%s: %v; want the CHECK to refuse it", name, err)
		}
	}
}
