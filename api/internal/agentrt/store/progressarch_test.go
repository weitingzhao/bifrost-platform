package store_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The architecture table on GET /progress (W-31 2.5 G2 A): each row's tick
// is its done rule evaluated now against what the controller last recorded
// each environment runs; its note is its latest note event.

const archSpec = "8c5b6d589310a295878350a310b92a13f43a7de31b159dcfacc533169eb8de78"

func archGates(env string, tests ...string) string {
	var gates []string
	for _, t := range tests {
		gates = append(gates, `{"test":"`+t+`","spec_sha256":"`+archSpec+`"}`)
	}
	return `{"gates_passed":[` + strings.Join(gates, ",") + `],"env":"` + env + `"}`
}

// arch makes the ARCH root and its rows in order, each [id, rule] with its
// rule set by a plan change when there is one.
func (w *world) arch(t *testing.T, rows ...[2]string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "ARCH", Title: "架构对照",
			Root: "ARCH"}); err != nil {
			return err
		}
		for i, r := range rows {
			label := strings.TrimPrefix(r[0], "ARCH:")
			if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: r[0], Parent: "ARCH", Kind: "arch_row", Label: label,
				Title: label, Ordinal: i, Root: "ARCH"}); err != nil {
				return err
			}
		}
		return nil
	}))
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("ARCH"), Type: "plan_changed", Actor: "controller",
				Body: map[string]any{"change": "set_done_rule"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, r[0], r[1])
			return err
		}))
	}
}

// archRows is the ARCH root's rows on GET /progress, by id.
func (w *world) archRows(t *testing.T) map[string]map[string]any {
	t.Helper()
	for _, r := range w.progress(t).Plan {
		if r.PlanNodeID != "ARCH" {
			continue
		}
		out := map[string]map[string]any{}
		for _, row := range r.Rows {
			out[row["plan_node_id"].(string)] = row
		}
		return out
	}
	t.Fatal("no ARCH root on /progress")
	return nil
}

func wantTick(t *testing.T, row map[string]any, tick, reason string) {
	t.Helper()
	if row["tick"] != tick || !strings.Contains(row["reason"].(string), reason) {
		t.Fatalf("row %v: tick %v (%v); want %s with %q", row["plan_node_id"], row["tick"], row["reason"], tick, reason)
	}
}

// T33 on the page: a gate counts only for what the environment runs as last
// recorded; nothing recorded is unknown, a new deployment takes the tick
// away, the old one back gives it back; a row with no rule is unknown.
func TestArchTickFollowsTheDeployment(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.arch(t, [2]string{"ARCH:state-db", archGates("stg", "T19", "T20")}, [2]string{"ARCH:gateway", ""},
		[2]string{"ARCH:prod-only", archGates("prod", "T19")})
	rows := w.archRows(t)
	if len(rows) != 3 {
		t.Fatalf("rows = %v", rows)
	}
	wantTick(t, rows["ARCH:gateway"], "unknown", store.ReasonNoGate)
	if rows["ARCH:gateway"]["reason"] != store.ReasonNoGate || rows["ARCH:gateway"]["note"] != nil || rows["ARCH:gateway"]["title"] != "gateway" {
		t.Fatalf("a row with no rule: %v", rows["ARCH:gateway"])
	}
	wantTick(t, rows["ARCH:state-db"], "unknown", "T19, T20: what stg runs is not recorded yet")

	refA := map[string]any{"run": "deliver-1", "digests": []any{"sha256:aa"}}
	refB := map[string]any{"run": "deliver-2", "digests": []any{"sha256:bb"}}
	deploy := func(env string, ref map[string]any) {
		t.Helper()
		_, err := store.SeeDeploy(ctx, w.rt, env, deployedIs(commitA, ref))
		ok(t, err)
	}
	deploy("stg", refA)
	// The rule stops at the first gate that fails: the reason names it.
	wantTick(t, w.archRows(t)["ARCH:state-db"], "false", "T19: no pass on stg at 2333e00753f9, the deployment recorded 20")

	task := store.NewTaskID()
	var attempt string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: task, MissionID: "M-1", PlanNode: s1, Role: "implement",
			Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "none"}); err != nil {
			return err
		}
		var err error
		attempt, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", task, "exec:deterministic", "", 1, time.Minute)
		return err
	}))
	gate := func(test, env, commit string, ref map[string]any) {
		t.Helper()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			ev, err := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte(test+env+commit+ref["run"].(string)))
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, check_ref, env, verdict,
			  evidence_artifact_id, subject_commit, deploy_ref_json, spec_sha256) VALUES ($1,$2,$3,'gate',$4,$5,'pass',$6,$7,$8,$9)`,
				store.NewVerificationID(), task, attempt, test, env, ev, commit, ref, archSpec)
			return err
		}))
	}
	gate("T19", "stg", commitA, refA)
	gate("T20", "stg", commitB, refA)
	rows = w.archRows(t)
	wantTick(t, rows["ARCH:state-db"], "false", "T20: no pass on stg at 2333e00753f9")
	if strings.Contains(rows["ARCH:state-db"]["reason"].(string), "T19") {
		t.Fatalf("a gate that passed is named as failing: %v", rows["ARCH:state-db"]["reason"])
	}
	// The same pass in another environment counts for nothing there.
	wantTick(t, rows["ARCH:prod-only"], "unknown", "T19: what prod runs is not recorded yet")
	gate("T20", "stg", commitA, refA)
	wantTick(t, w.archRows(t)["ARCH:state-db"], "true", "T19, T20 passed on stg at 2333e00753f9")
	deploy("stg", refB)
	wantTick(t, w.archRows(t)["ARCH:state-db"], "false", "T19: no pass on stg")
	deploy("stg", refA)
	wantTick(t, w.archRows(t)["ARCH:state-db"], "true", "T19, T20 passed on stg")
	// Evaluating changes nothing: the projection only reads.
	var moved int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE plan_node_id LIKE 'ARCH:%' AND event_type <> 'plan_node_created'`).Scan(&moved))
	if moved != 0 {
		t.Fatalf("%d events on the rows after reading the page", moved)
	}
}

// A rule the row cannot be ticked by says so on the row, and the rest of the
// page is still read.
func TestArchRowWithARuleThatDoesNotParse(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.arch(t, [2]string{"ARCH:odd", `{"some_future_atom":1}`})
	wantTick(t, w.archRows(t)["ARCH:odd"], "unknown", "its done rule does not parse")
}

// Notes on rows: append only, the latest shown, held to an agent text's
// rules, only on architecture rows, one per key.
func TestPlanNotes(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.arch(t, [2]string{"ARCH:state-db", ""}, [2]string{"ARCH:bus", ""})
	thread := store.Writer{Actor: "thread:night", Token: "fixture-operator-1"}

	first, err := store.PostPlanNote(ctx, w.rt, thread, "n1", "ARCH:state-db", "PROD and STG both use it")
	ok(t, err)
	if first["by"] != "thread:night" || first["plan_node_id"] != "ARCH:state-db" || first["event_id"] == nil || first["at"] == "" {
		t.Fatalf("first note = %v", first)
	}
	again, err := store.PostPlanNote(ctx, w.rt, thread, "n1", "ARCH:state-db", "PROD and STG both use it")
	ok(t, err)
	if again["event_id"] != first["event_id"] {
		t.Fatalf("a retry wrote another note: %v, then %v", first, again)
	}
	_, err = store.PostPlanNote(ctx, w.rt, thread, "n1", "ARCH:state-db", "something else")
	is(t, err, store.ErrConflict)
	later, err := store.PostPlanNote(ctx, w.rt, owner, "n2", "ARCH:state-db", "PROD rebuild drill done")
	ok(t, err)
	rows := w.archRows(t)
	note, _ := rows["ARCH:state-db"]["note"].(map[string]any)
	if note == nil || note["text"] != "PROD rebuild drill done" || note["by"] != "owner" || note["at"] != later["at"] {
		t.Fatalf("the latest note = %v; want the owner's, at %v", rows["ARCH:state-db"]["note"], later["at"])
	}
	if rows["ARCH:bus"]["note"] != nil {
		t.Fatalf("another row's note shows on bus: %v", rows["ARCH:bus"]["note"])
	}
	var notes int
	var stream string
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*), min(event_stream_id) FROM agentrt.event WHERE event_type = 'note' AND plan_node_id = 'ARCH:state-db'`).Scan(&notes, &stream))
	if notes != 2 || stream != "plan:ARCH" {
		t.Fatalf("%d notes on %s; want 2 on plan:ARCH", notes, stream)
	}
	w.chainHolds(t, "plan:ARCH")

	for name, c := range map[string]struct {
		node, text string
		want       error
	}{
		"a step":            {s1, "x", store.ErrInvalid},
		"a round":           {root, "x", store.ErrInvalid},
		"the root":          {"ARCH", "x", store.ErrInvalid},
		"no such node":      {"ARCH:nothing", "x", pgx.ErrNoRows},
		"empty":             {"ARCH:bus", "", store.ErrInvalid},
		"white space":       {"ARCH:bus", " \n\t", store.ErrInvalid},
		"too long":          {"ARCH:bus", strings.Repeat("长", store.MaxNoteText/3+1), store.ErrInvalid},
		"not UTF-8":         {"ARCH:bus", "a\xffb", store.ErrInvalid},
		"a NUL":             {"ARCH:bus", "a\x00b", store.ErrInvalid},
		"a local path":      {"ARCH:bus", "see /Users/someone/notes.md", store.ErrInvalid},
		"a home path":       {"ARCH:bus", "see ~/agent-work/status", store.ErrInvalid},
		"a task workspace":  {"ARCH:bus", "read tasks/t_01JABCDEFGHJKMNPQRSTVWXYZ0/a1/out", store.ErrInvalid},
		"no idempotent key": {"ARCH:bus", "x", store.ErrInvalid},
	} {
		key := "k-" + name
		if name == "no idempotent key" {
			key = ""
		}
		if _, perr := store.PostPlanNote(ctx, w.rt, thread, key, c.node, c.text); !errors.Is(perr, c.want) {
			t.Errorf("%s: err = %v; want %v", name, perr, c.want)
		}
	}
	// At the bound it is taken.
	_, err = store.PostPlanNote(ctx, w.rt, thread, "k-bound", "ARCH:bus", strings.Repeat("a", store.MaxNoteText))
	ok(t, err)
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND plan_node_id = 'ARCH:bus'`).Scan(&notes))
	if notes != 1 {
		t.Fatalf("%d notes on bus; only the one at the bound was to be written", notes)
	}
}

// The table imported beside round 2: rows with the rules the request gives,
// what each line says it is now as the row's first note, the round as it
// was; the same request again changes nothing.
func TestArchImportBesideTheRound(t *testing.T) {
	t.Parallel()
	pool, round := importDB(t)
	_, err := store.ImportPlan(ctx, pool, "controller", round)
	ok(t, err)
	text, err := os.ReadFile("../planimport/testdata/ledger.md")
	ok(t, err)
	gates := archGates("stg", "T05")
	arch, err := planimport.ParseArch(string(text), []planimport.ArchRow{{Label: "release-guard", Rule: []byte(gates)}, {Label: "state-db"}},
		planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	res, err := store.ImportPlan(ctx, pool, "controller", arch)
	ok(t, err)
	if !res.Imported || res.Root != "ARCH" || res.Nodes != 3 || res.Done != 0 || res.Active != 0 {
		t.Fatalf("import = %+v", res)
	}
	var rule, empty string
	ok(t, pool.QueryRow(ctx, `SELECT (SELECT done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = 'ARCH:release-guard'),
	  (SELECT done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = 'ARCH:state-db')`).Scan(&rule, &empty))
	if !strings.Contains(rule, `"test": "T05"`) || empty != "{}" {
		t.Fatalf("rules = %s, %s", rule, empty)
	}
	var cause, head int64
	var noteText, evidence string
	ok(t, pool.QueryRow(ctx, `SELECT n.body_json->>'text', n.body_json->'evidence'->>0, n.cause_event_id, h.event_id
	  FROM agentrt.event n, agentrt.event h WHERE n.event_type = 'note' AND n.plan_node_id = 'ARCH:state-db'
	    AND h.event_type = 'legacy_imported' AND h.event_stream_id = 'plan:ARCH'`).Scan(&noteText, &evidence, &cause, &head))
	if noteText != "两个环境都在用" || evidence != res.Request || cause != head {
		t.Fatalf("first note %q, evidence %s, caused by %d; want the line's text, the request %s, the head %d", noteText, evidence, cause, res.Request, head)
	}
	out, err := store.Progress(ctx, pool)
	ok(t, err)
	var kinds []string
	for _, r := range out["plan"].([]any) {
		doc := r.(map[string]any)
		kinds = append(kinds, doc["plan_node_id"].(string)+"/"+doc["kind"].(string))
		if doc["kind"] != "arch_root" {
			continue
		}
		rows := doc["rows"].([]any)
		first, second := rows[0].(map[string]any), rows[1].(map[string]any)
		if first["tick"] != "unknown" || !strings.Contains(first["reason"].(string), "what stg runs is not recorded yet") ||
			first["note"].(map[string]any)["text"] != "在正式环境" || second["reason"] != store.ReasonNoGate {
			t.Fatalf("rows = %v", rows)
		}
	}
	if strings.Join(kinds, ",") != "ARCH/arch_root,R2/round" {
		t.Fatalf("roots = %v", kinds)
	}
	again, err := store.ImportPlan(ctx, pool, "controller", arch)
	ok(t, err)
	if again.Imported || again.EventID != res.EventID {
		t.Fatalf("the same request again = %+v", again)
	}
}

// A line whose text a note may not hold refuses the whole import.
func TestArchImportRefusesALocalPath(t *testing.T) {
	t.Parallel()
	pool, _ := importDB(t)
	text := "### W-31\n  - **架构对照（核对于 2026-10-19 01:20）**\n    - [~] 看板｜在 /Users/someone/agent-work/status 上跑\n"
	arch, err := planimport.ParseArch(text, []planimport.ArchRow{{Label: "board"}}, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	before := rows(t, pool)
	_, err = store.ImportPlan(ctx, pool, "controller", arch)
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "a local path in the note") {
		t.Fatalf("err = %v", err)
	}
	if after := rows(t, pool); after != before {
		t.Fatalf("a refused import wrote: %v, then %v", before, after)
	}
}
