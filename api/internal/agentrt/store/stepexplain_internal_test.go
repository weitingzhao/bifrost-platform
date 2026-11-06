package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// R5:5.5-19 §4.4: the STG blockers follow stgSettled's own order, so a
// member has a blocker exactly when stgSettled is not true, and its state
// names the branch stgSettled took.

func exMember(root string) StgMemberKey {
	return StgMemberKey{Root: root, Op: "land:" + root + ":1", R: "r-" + root}
}

func exExplain(s StgState) *repoExplain {
	return &repoExplain{stg: &s, stgTriggers: stgTriggers(s)}
}

// Every case of TestStgSettled: no blocker for true, and the branch.
func TestExplainSTGFollowsStgSettled(t *testing.T) {
	t.Parallel()
	m := StgMemberKey{Root: evRoot, Op: "land:" + evRoot + ":1", R: "r1"}
	state := func(phase, to string, admitted, covered, withheld bool) StgState {
		s := newStgState()
		s.Batches["b1"] = &StgBatch{Phase: phase}
		if admitted {
			s.Members[m] = StgMemberState{Batch: "b1", To: to}
		}
		if covered {
			s.Covers[StgCoverKey{Evidence: "cluster/cicd/run", Member: m}] = StgEvent{Seq: 9}
		}
		if withheld {
			s.Withheld[m] = StgEvent{Seq: 7, Record: StgRecord{"why": "owner"}}
		}
		return s
	}
	missing := state("CLOSED", "", true, false, false)
	delete(missing.Batches, "b1")
	for name, c := range map[string]struct {
		s    StgState
		want string // "" for no blocker
	}{
		"never admitted":                  {state("CLOSED", "", false, false, false), "stg_not_admitted"},
		"in an open batch":                {state("SNAP", "", true, false, false), "stg_batch_open"},
		"delivered, batch not settled":    {state("VERIFIED", "delivered", true, false, false), "stg_batch_open"},
		"delivered, batch settled":        {state("CLOSED", "delivered", true, false, false), ""},
		"covered, batch settled":          {state("CLOSED", "covered", true, true, false), ""},
		"covered, never admitted":         {state("CLOSED", "", false, true, false), ""},
		"released, batch settled":         {state("CLOSED", "released", true, false, false), "stg_not_admitted"},
		"released, then covered":          {state("CLOSED", "released", true, true, false), ""},
		"exhausted":                       {state("CLOSED", "exhausted", true, false, false), "stg_exhausted"},
		"disposed":                        {state("CLOSED", "disposed", true, false, false), "stg_disposed"},
		"withheld":                        {state("CLOSED", "", false, false, true), "stg_withheld"},
		"withheld, though in a batch":     {state("SNAP", "", true, false, true), "stg_withheld"},
		"admitted to a batch not in fold": {missing, "stg_batch_open"},
	} {
		b := exExplain(c.s).stgMember(m)
		got := ""
		if b != nil {
			got, _ = b["state"].(string)
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q (%v)", name, got, c.want, b)
		}
		if (b == nil) != (stgSettled(c.s, m).String() == "true") {
			t.Errorf("%s: blocker %v beside stgSettled %s", name, b, stgSettled(c.s, m))
		}
		if b != nil {
			if _, ok := b["blocks"]; !ok {
				t.Errorf("%s: a member blocker without blocks: %v", name, b)
			}
			if _, ok := b["repo_blocks"]; ok != (c.want == "stg_not_admitted") {
				t.Errorf("%s: repo_blocks only on not_admitted: %v", name, b)
			}
		}
	}
	if b := exExplain(missing).stgMember(m); b["batch_state"] != "missing" || b["try"] != nil || b["quarantine"] != nil {
		t.Errorf("a batch not in the fold: %v", b)
	}
}

// exBlock is an open blocking interval naming members.
func exBlock(seq int64, at time.Time, why string, members ...StgMemberKey) StgBlock {
	var ms []any
	for _, m := range members {
		ms = append(ms, map[string]any{"root": m.Root, "op": m.Op, "R": m.R})
	}
	return StgBlock{Opened: StgEvent{Seq: seq, At: at, Record: StgRecord{"why": why, "first_at": at.Format(time.RFC3339Nano),
		"detail": map[string]any{"members": ms}}}}
}

func exStgCard(prep, kind, block string, seq int64, n int) CardRecord {
	return CardRecord{Prep: prep, Family: "stg", Kind: kind, Block: block, TriggerSeq: seq, N: n}
}

// Review round 9: exhausted after the third try, the batch CLOSED, and the
// member's ci_red and budget_exhausted intervals still open with no card
// yet: both blocks are listed. Round 8: another member B waiting for a new
// batch, named by no block, sees every open block in repo_blocks, each
// with its own trigger's cards.
func TestExplainSTGBlocks(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	a, b, c := exMember("t_a"), exMember("t_b"), exMember("t_c")
	s := newStgState()
	s.Batches["stg:bifrost-platform:1"] = &StgBatch{Phase: "CLOSED"}
	s.Members[a] = StgMemberState{Batch: "stg:bifrost-platform:1", To: "exhausted"}
	s.Blocks["ci"] = exBlock(10, at.Add(time.Minute), "ci_red", a)
	s.Blocks["budget"] = exBlock(11, at, "budget_exhausted", a)
	s.Blocks["other"] = exBlock(12, at.Add(2*time.Minute), "ci_red", c)
	ex := exExplain(s)
	got := ex.stgMember(a)
	if got["state"] != "stg_exhausted" {
		t.Fatalf("exhausted member: %v", got)
	}
	ids := func(v any) []string {
		var out []string
		for _, b := range v.([]map[string]any) {
			out = append(out, b["id"].(string))
		}
		return out
	}
	if g := ids(got["blocks"]); !reflect.DeepEqual(g, []string{"budget", "ci"}) {
		t.Fatalf("exhausted member's blocks %v, want its two by since", g)
	}
	for _, blk := range got["blocks"].([]map[string]any) {
		if len(blk["cards"].([]map[string]any)) != 0 {
			t.Fatalf("no card yet, got %v", blk)
		}
	}
	// B waits for a new batch: no block names it; every open block holds
	// the repository. Cards of the ci block's own trigger, newest first,
	// three at most; a card of an older trigger of the same block is not one.
	ex.cards = []CardRecord{exStgCard("p1", CardSTGBlocked, "other", 12, 1), exStgCard("p2", CardSTGBlocked, "other", 12, 2),
		exStgCard("p3", CardSTGBlocked, "other", 12, 3), exStgCard("p4", CardSTGBlocked, "other", 12, 4), exStgCard("old", CardSTGBlocked, "other", 5, 1)}
	ex.indexCards()
	got = ex.stgMember(b)
	if got["state"] != "stg_not_admitted" || len(got["blocks"].([]map[string]any)) != 0 {
		t.Fatalf("member named by no block: %v", got)
	}
	if g := ids(got["repo_blocks"]); !reflect.DeepEqual(g, []string{"budget", "ci", "other"}) {
		t.Fatalf("repo_blocks %v", g)
	}
	other := got["repo_blocks"].([]map[string]any)[2]
	var preps []string
	for _, c := range other["cards"].([]map[string]any) {
		preps = append(preps, c["prep"].(string))
	}
	if !reflect.DeepEqual(preps, []string{"p4", "p3", "p2"}) || other["cards_omitted"] != 1 {
		t.Fatalf("cards of the block's trigger: %v (%v omitted)", preps, other["cards_omitted"])
	}
	if !reflect.DeepEqual(got["unexplained"], []string{"budget", "policy", "repo_cards"}) || got["to"] != nil {
		t.Fatalf("not admitted: %v", got)
	}
	// More open blocks than the bound: five, and the rest counted.
	for i := 0; i < 5; i++ {
		s.Blocks[string(rune('k'+i))] = exBlock(int64(20+i), at.Add(time.Duration(10+i)*time.Minute), "ci_red", a)
	}
	got = exExplain(s).stgMember(a)
	if len(got["blocks"].([]map[string]any)) != explainBlocks || got["blocks_omitted"] != 2 {
		t.Fatalf("seven blocks of a: %v", got)
	}
}

// An open batch: try is null before any snapshot; a snapshot not started
// has started_at null; a quarantined try carries its reason and the cards
// of its own trigger.
func TestExplainSTGBatchOpen(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	m := exMember("t_m")
	s := newStgState()
	bt := &StgBatch{Phase: "OPEN", Tries: map[int64]*StgTry{}}
	s.Batches["stg:bifrost-platform:2"] = bt
	s.Members[m] = StgMemberState{Batch: "stg:bifrost-platform:2"}
	if got := exExplain(s).stgMember(m); got["try"] != nil || got["batch_state"] != "OPEN" {
		t.Fatalf("no try: %v", got)
	}
	bt.Phase, bt.Try = "SNAP", 1
	bt.Tries[1] = &StgTry{Snapshot: StgEvent{Seq: 30, At: at}}
	tr := exExplain(s).stgMember(m)["try"].(map[string]any)
	if tr["n"] != int64(1) || tr["snapshot_at"] == nil || tr["started_at"] != nil || tr["bound_uid"] != nil || tr["run"] != nil {
		t.Fatalf("snapshot only: %v", tr)
	}
	bt.Tries[1].Started = StgEvent{Seq: 31, At: at.Add(time.Minute), Record: StgRecord{"run": "deliver-1"}}
	bt.Tries[1].Quarantined = StgEvent{Seq: 33, At: at.Add(2 * time.Minute), Record: StgRecord{"why": "unreconciled"}}
	ex := exExplain(s)
	ex.cards = []CardRecord{{Prep: "q1", Family: "stg", Kind: CardSTGBlocked, Batch: "stg:bifrost-platform:2", TriggerSeq: 33, N: 1, QuestionID: "q_1"}}
	ex.indexCards()
	got := ex.stgMember(m)
	q, _ := got["quarantine"].(map[string]any)
	if q == nil || q["reason"] != "unreconciled" || q["trigger_seq"] != int64(33) || len(q["cards"].([]map[string]any)) != 1 {
		t.Fatalf("quarantine: %v", got)
	}
	if tr := got["try"].(map[string]any); tr["started_at"] == nil || tr["run"] != "deliver-1" {
		t.Fatalf("started: %v", tr)
	}
}

// A card's durable fields as they are: an empty question id is null, the
// reason only for a closed card.
func TestExplainCardOut(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		in   CardRecord
		want map[string]any
	}{
		"prepared": {CardRecord{Prep: "p", Kind: "root_failed"},
			map[string]any{"question_id": nil, "prep": "p", "kind": "root_failed", "family": "landing", "ask_started": false, "closed": false, "closed_why": nil}},
		"ask started, closed resolved": {CardRecord{Prep: "p", Kind: "root_failed", AskStarted: true, Closed: true, ClosedWhy: "resolved"},
			map[string]any{"question_id": nil, "prep": "p", "kind": "root_failed", "family": "landing", "ask_started": true, "closed": true, "closed_why": "resolved"}},
		"bound stg": {CardRecord{Prep: "p", Kind: CardSTGBlocked, Family: "stg", AskStarted: true, Bound: true, QuestionID: "q_9"},
			map[string]any{"question_id": "q_9", "prep": "p", "kind": CardSTGBlocked, "family": "stg", "ask_started": true, "closed": false, "closed_why": nil}},
	} {
		if got := cardOut(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// exNone reads every atom false.
type exNone struct{}

func (exNone) TaskAccepted(string) bool                   { return false }
func (exNone) ApprovalExecuted(int64) bool                { return false }
func (exNone) QuestionAnswered(string, string) bool       { return false }
func (exNone) CISucceeded(string, string) bool            { return false }
func (exNone) OwnerSignoff(string) bool                   { return false }
func (exNone) ManualRecorded(string) bool                 { return false }
func (exNone) GatePassed(string, string, string) plan.Tri { return plan.False }
func (exNone) StepTasks(string, int64) plan.Tri           { return plan.False }
func (exNone) StepLanded() plan.Tri                       { return plan.False }
func (exNone) StepSTGSettled() plan.Tri                   { return plan.False }

// Review round 11 M2: the 50-item budget is spent in rule order, inside a
// one_of's branches too, and every blocker left out is counted; a one_of
// with nothing left in any branch is dropped and its blockers counted.
func TestExplainBudget(t *testing.T) {
	t.Parallel()
	pending := func(n int) []Blocker {
		out := make([]Blocker, n)
		for i := range out {
			out[i] = Blocker{"atom": plan.AtomStepLanded, "state": "landing_failed", "task_id": "t", "op": "o", "outcome": "stale", "cards": []map[string]any{}}
		}
		return out
	}
	run := func(doc string, by map[string]int) ([]Blocker, int) {
		t.Helper()
		r, err := plan.Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		f := &stepFacts{ctx: context.Background(), atomBlockers: func(n *plan.Node) []Blocker { return pending(by[n.Kind]) }}
		node, ok, _ := r.Explain(exNone{})
		if !ok {
			t.Fatalf("%s not supported", doc)
		}
		return stepBlockers(f, node.Blocking())
	}
	// any(step_landed) with 51: one one_of, 50 shown, 1 counted.
	out, omitted := run(`{"any":[{"step_landed":{}}]}`, map[string]int{plan.AtomStepLanded: 51})
	if len(out) != 1 || len(out[0]["branches"].([][]Blocker)[0]) != 50 || omitted != 1 {
		t.Fatalf("any of 51: %d blockers, %d omitted", len(out), omitted)
	}
	// all(landed 49, stg 2): 49 + 1 shown, the rest counted.
	out, omitted = run(`{"all":[{"step_landed":{}},{"step_stg_settled":{}}]}`, map[string]int{plan.AtomStepLanded: 49, plan.AtomStepSTG: 2})
	if len(out) != 50 || omitted != 1 {
		t.Fatalf("all of 49 and 2: %d shown, %d omitted", len(out), omitted)
	}
	// all(landed 50, any(stg 3)): the one_of has nothing left: dropped, counted.
	out, omitted = run(`{"all":[{"step_landed":{}},{"any":[{"step_stg_settled":{}}]}]}`, map[string]int{plan.AtomStepLanded: 50, plan.AtomStepSTG: 3})
	if len(out) != 50 || omitted != 3 {
		t.Fatalf("a full budget before a one_of: %d shown, %d omitted", len(out), omitted)
	}
	for _, b := range out {
		if b["atom"] == "one_of" {
			t.Fatal("an empty one_of was kept")
		}
	}
	// Under the budget: nothing counted.
	if _, omitted = run(`{"step_landed":{}}`, map[string]int{plan.AtomStepLanded: 3}); omitted != 0 {
		t.Fatalf("3 blockers: %d omitted", omitted)
	}
}

// Review round 12 M3: the budget walk stops at once when the explanation's
// context ends, inside one atom's blockers too, and says so.
func TestExplainBudgetStopsOnCancel(t *testing.T) {
	t.Parallel()
	c, cancel := context.WithCancel(context.Background())
	f := &stepFacts{ctx: c, atomBlockers: func(*plan.Node) []Blocker {
		out := make([]Blocker, 2000)
		for i := range out {
			out[i] = Blocker{"atom": plan.AtomStepLanded, "state": "no_landing_evidence", "task_id": "t"}
		}
		cancel() // ends while the atom's blockers are walked
		return out
	}}
	r, err := plan.Parse([]byte(`{"step_landed":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	n, _, _ := r.Explain(exNone{})
	out, omitted := stepBlockers(f, n.Blocking())
	if !errors.Is(f.err, context.Canceled) || out != nil || omitted != 0 {
		t.Fatalf("after a cancel: err %v, %d shown, %d omitted", f.err, len(out), omitted)
	}
	ex := &repoExplain{ctx: c, stg: &StgState{Blocks: map[string]StgBlock{"b": exBlock(1, time.Now(), "ci_red")}}}
	if blocks, _ := ex.stgBlocks(func(StgRecord) bool { return true }); len(blocks) != 0 {
		t.Fatalf("blocks walked after a cancel: %v", blocks)
	}
}

// Review round 13 M1: the root card match stops when the explanation's
// context ends, however many triggers the repository has.
func TestExplainRootCardsStopOnCancel(t *testing.T) {
	t.Parallel()
	c, cancel := context.WithCancel(context.Background())
	ex := &repoExplain{ctx: c}
	for i := 0; i < 2000; i++ {
		ex.triggers = append(ex.triggers, CardTrigger{Kind: CardRootFailed, Seq: int64(i + 1), Root: "t_x", Op: "land:t_x:1"})
	}
	ex.cards = []CardRecord{{Prep: "last", Kind: CardRootFailed, TriggerSeq: 2000, Root: "t_x", Op: "land:t_x:1", N: 1}}
	ex.indexCards()
	f := &stepFacts{ctx: c, repos: map[string]*repoExplain{"bifrost-platform": ex}}
	cancel()
	b := Blocker{}
	f.addRootCards(b, implRoot{task: "t_x", repo: "bifrost-platform", op: "land:t_x:1"})
	if !errors.Is(f.err, context.Canceled) || b["cards"] != nil {
		t.Fatalf("after a cancel: err %v, cards %v", f.err, b["cards"])
	}
	// The index stops too: built after the cancel, it holds nothing.
	ex.indexCards()
	if len(ex.byTrigger) != 0 {
		t.Fatalf("indexed %d groups after a cancel", len(ex.byTrigger))
	}
}
