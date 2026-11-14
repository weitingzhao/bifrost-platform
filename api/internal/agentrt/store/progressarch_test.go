package store_test

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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
	ctx := ctxOf(t)
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

// The coordinator, 2026-11-14: gates are no longer run, so a row whose rule
// names gates is unknown and says why, whatever any environment runs; a row
// with no rule is unknown as before.
func TestArchGatesReadUnknown(t *testing.T) {
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
	wantTick(t, rows["ARCH:state-db"], "unknown", "T19, T20: "+store.ReasonGatesRetired+" (stg)")
	wantTick(t, rows["ARCH:prod-only"], "unknown", "T19: "+store.ReasonGatesRetired+" (prod)")
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
		if first["tick"] != "unknown" || !strings.Contains(first["reason"].(string), store.ReasonGatesRetired) ||
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

// Review 427 A2: an architecture row is ticked by gate tests only. A rule
// that names a fact which stays true once recorded is unknown on the page,
// and a plan change that would give a row one is refused: when proposed
// (its trial apply) and so before any card reaches the Owner.
func TestArchRowRuleIsGatesOnly(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.arch(t, [2]string{"ARCH:state-db", `{"ci_succeeded":{"repo":"bifrost-platform","commit":"` + commitA + `"}}`}, [2]string{"ARCH:bus", ""})
	wantTick(t, w.archRows(t)["ARCH:state-db"], "unknown", store.ReasonNotGates)

	a := &asker{}
	propose := func(key, rule string) (map[string]any, error) {
		return store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, "ARCH", "test",
			[]store.PlanChange{{Op: "set_done_rule", Node: "ARCH:bus", Rule: []byte(rule)}}, a.ask)
	}
	if _, err := propose("k-ci", `{"ci_succeeded":{"repo":"bifrost-platform","commit":"`+commitA+`"}}`); err == nil ||
		!strings.Contains(err.Error(), "gate tests only") {
		t.Fatalf("a CI rule on an architecture row was proposed: %v", err)
	}
	if len(a.cards) != 0 {
		t.Fatalf("a refused change reached the Owner: %v", a.cards)
	}
	out, err := propose("k-gates", gateRule("stg", [2]string{"T21", strings.Repeat("ab", 32)}))
	ok(t, err)
	gates := out["question_id"].(string)
	a.answer(gates, store.QuestionAnswered, store.CardApplyKey)
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	if n != 1 {
		t.Fatalf("decided %d; want 1", n)
	}
	decided := map[string][2]string{}
	rows, err := w.rt.Query(ctx, `SELECT body_json->>'question_id', body_json->>'decision', COALESCE(body_json->>'error', '')
	  FROM agentrt.event WHERE event_type = 'plan_change_decided'`)
	ok(t, err)
	for rows.Next() {
		var q, d, e string
		ok(t, rows.Scan(&q, &d, &e))
		decided[q] = [2]string{d, e}
	}
	ok(t, rows.Err())
	if d := decided[gates]; d[0] != "applied" {
		t.Fatalf("a gates rule on an architecture row: %v", d)
	}
	wantTick(t, w.archRows(t)["ARCH:bus"], "unknown", "T21: "+store.ReasonGatesRetired+" (stg)")
}

// Review 427 C1, 429: the latest note is the one appended last (event_id),
// whatever its time says.
func TestArchLatestNoteIsTheLastAppended(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w := newWorld(t)
	w.arch(t, [2]string{"ARCH:state-db", ""})
	thread := store.Writer{Actor: "thread:night", Token: "fixture-operator-1"}
	later := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	restore := freezeClock(t, later)
	first, err := store.PostPlanNote(ctx, w.rt, thread, "n1", "ARCH:state-db", "written first, stamped later")
	restore()
	ok(t, err)
	restore = freezeClock(t, later.Add(-time.Hour))
	second, err := store.PostPlanNote(ctx, w.rt, thread, "n2", "ARCH:state-db", "written second, stamped earlier")
	restore()
	ok(t, err)
	id := func(v any) int64 {
		n, err := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		ok(t, err)
		return n
	}
	if id(second["event_id"]) <= id(first["event_id"]) || second["at"].(string) >= first["at"].(string) {
		t.Fatalf("not staged as wanted: first %v, second %v", first, second)
	}
	note, _ := w.archRows(t)["ARCH:state-db"]["note"].(map[string]any)
	if note == nil || note["text"] != "written second, stamped earlier" {
		t.Fatalf("the latest note = %v; want the one appended last", note)
	}
}

// gateRule is a gates_passed rule of env naming each gate's test and spec.
func gateRule(env string, gates ...[2]string) string {
	var list []string
	for _, g := range gates {
		list = append(list, `{"test":"`+g[0]+`","spec_sha256":"`+g[1]+`"}`)
	}
	return `{"gates_passed":[` + strings.Join(list, ",") + `],"env":"` + env + `"}`
}
