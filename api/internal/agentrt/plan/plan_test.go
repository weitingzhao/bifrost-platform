package plan

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const (
	taskA  = "t_01JABCDEFGHJKMNPQRSTVWXYZ0"
	taskB  = "t_01JABCDEFGHJKMNPQRSTVWXYZ1"
	commit = "2333e00753f91c74c8b2f4d693a803f8e89e7892"
	spec   = "8c5b6d589310a295878350a310b92a13f43a7de31b159dcfacc533169eb8de78"
)

type facts struct {
	accepted map[string]bool
	approved map[int64]bool
	answered map[string]bool
	ci       map[string]bool
	signoff  map[string]bool
	manual   map[string]bool
	gates    map[string]Tri // env/test/spec
}

func (f facts) TaskAccepted(id string) bool       { return f.accepted[id] }
func (f facts) ApprovalExecuted(n int64) bool     { return f.approved[n] }
func (f facts) QuestionAnswered(q, o string) bool { return f.answered[q+"/"+o] }
func (f facts) CISucceeded(repo, c string) bool   { return f.ci[repo+"@"+c] }
func (f facts) OwnerSignoff(ref string) bool      { return f.signoff[ref] }
func (f facts) ManualRecorded(note string) bool   { return f.manual[note] }
func (f facts) GatePassed(env, test, specSHA string) Tri {
	if v, ok := f.gates[env+"/"+test+"/"+specSHA]; ok {
		return v
	}
	return False
}

func mustParse(t *testing.T, doc string) Rule {
	t.Helper()
	r, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse(%s): %v", doc, err)
	}
	return r
}

func TestGrammarIsClosed(t *testing.T) {
	bad := []string{
		`[]`,
		`{"all":[]}`,
		`{"any":[1]}`,
		`{"task_accepted":"t_short"}`,
		`{"task_accepted":"t_01JABCDEFGHJKMNPQRSTVWXYZ0","note":"x"}`,
		`{"approval_executed":0}`,
		`{"approval_executed":"16"}`,
		`{"approval_executed":1.5}`,
		`{"question_answered":{"question":"Q3"}}`,
		`{"question_answered":{"question":"Q3","option":"A","extra":1}}`,
		`{"ci_succeeded":{"repo":"bifrost-platform","commit":"2333e00"}}`,
		`{"owner_signoff":""}`,
		`{"manual":""}`,
		`{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"}]}`,
		`{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"}],"env":"qa"}`,
		`{"gates_passed":[{"test":"T19","spec_sha256":"short"}],"env":"prod"}`,
		`{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"},{"test":"T19","spec_sha256":"` + spec + `"}],"env":"prod"}`,
		`{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"}],"env":"prod","all":[]}`,
		`{"approved_by_owner":true}`,
		`{"all":[{"all":[{"all":[{"all":[{"all":[{"all":[{"all":[{"all":[{"all":[{"all":[{"manual":"x"}]}]}]}]}]}]}]}]}]}]}`,
		`{"a":1,"a":2}`,
	}
	for _, doc := range bad {
		if _, err := Parse([]byte(doc)); !errors.Is(err, ErrRule) {
			t.Errorf("Parse(%s) err = %v, want ErrRule", doc, err)
		}
	}
	if r := mustParse(t, `{}`); !r.Empty() || r.Eval(facts{}).Value != False {
		t.Fatal("an empty rule must never be satisfied")
	}
}

func TestEvalKleene(t *testing.T) {
	gateRule := `{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"},{"test":"T21","spec_sha256":"` + spec + `"}],"env":"prod"}`
	f := facts{
		accepted: map[string]bool{taskA: true},
		approved: map[int64]bool{17: true},
		answered: map[string]bool{"Q3/A": true},
		ci:       map[string]bool{"bifrost-platform@" + commit: true},
		signoff:  map[string]bool{"TD-338": true},
		manual:   map[string]bool{"drill done by hand": true},
		gates: map[string]Tri{
			"prod/T19/" + spec: True,
			"prod/T21/" + spec: Unknown,
		},
	}
	cases := []struct {
		doc    string
		want   Tri
		manual bool
	}{
		{`{"task_accepted":"` + taskA + `"}`, True, false},
		{`{"task_accepted":"` + taskB + `"}`, False, false},
		{`{"approval_executed":17}`, True, false},
		{`{"question_answered":{"question":"Q3","option":"B"}}`, False, false},
		{`{"ci_succeeded":{"repo":"bifrost-platform","commit":"` + commit + `"}}`, True, false},
		{`{"owner_signoff":"TD-338"}`, True, false},
		{`{"manual":"drill done by hand"}`, True, true},
		{gateRule, Unknown, false},
		{`{"all":[{"task_accepted":"` + taskA + `"},` + gateRule + `]}`, Unknown, false},
		{`{"all":[{"task_accepted":"` + taskB + `"},` + gateRule + `]}`, False, false},
		// An unreadable delivery record keeps the whole rule from done, even
		// beside a branch that holds (v5 §5.1.2, review 144 A4).
		{`{"any":[{"task_accepted":"` + taskA + `"},` + gateRule + `]}`, Unknown, false},
		{`{"any":[` + gateRule + `,{"task_accepted":"` + taskA + `"}]}`, Unknown, false},
		{`{"all":[{"approval_executed":17},{"any":[{"task_accepted":"` + taskA + `"},` + gateRule + `]}]}`, Unknown, false},
		{`{"any":[{"task_accepted":"` + taskB + `"},` + gateRule + `]}`, Unknown, false},
		{`{"all":[{"approval_executed":17},{"manual":"drill done by hand"}]}`, True, true},
		{`{"any":[{"approval_executed":17},{"manual":"drill done by hand"}]}`, True, false},
		{`{"any":[{"approval_executed":99},{"manual":"drill done by hand"}]}`, True, true},
	}
	for _, c := range cases {
		got := mustParse(t, c.doc).Eval(f)
		if got.Value != c.want || got.Manual != c.manual {
			t.Errorf("%s = %v manual=%v, want %v manual=%v", c.doc, got.Value, got.Manual, c.want, c.manual)
		}
	}
}

func TestGateUsesTheExactTestID(t *testing.T) {
	// A PASS recorded for T21 under the same spec must not satisfy T19
	// (review 142 B4); the store answers per test id, the rule asks per test.
	r := mustParse(t, `{"gates_passed":[{"test":"T19","spec_sha256":"`+spec+`"}],"env":"prod"}`)
	f := facts{gates: map[string]Tri{"prod/T21/" + spec: True}}
	if got := r.Eval(f).Value; got != False {
		t.Fatalf("T21's pass satisfied T19: %v", got)
	}
	if !r.Standing() || mustParse(t, `{"approval_executed":3}`).Standing() {
		t.Fatal("Standing must be true exactly for rules with a gate condition")
	}
}

func TestRefsListsEveryBoundObject(t *testing.T) {
	r := mustParse(t, `{"all":[{"task_accepted":"`+taskB+`"},{"any":[{"approval_executed":16},{"question_answered":{"question":"Q3","option":"A"}}]},{"owner_signoff":"TD-295"},{"task_accepted":"`+taskA+`"}]}`)
	want := []Ref{{RefApproval, "16"}, {RefSignoff, "TD-295"}, {RefQuestion, "Q3"}, {RefTask, taskA}, {RefTask, taskB}}
	if got := r.Refs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Refs = %v, want %v", got, want)
	}
}

func TestCountSteps(t *testing.T) {
	steps := func(states ...string) []Step {
		out := make([]Step, len(states))
		for i, s := range states {
			out[i] = Step{ID: string(rune('a' + i)), State: strings.TrimSuffix(s, "*"), Manual: strings.HasSuffix(s, "*")}
		}
		return out
	}
	cases := []struct {
		in   []Step
		want Count
	}{
		{nil, Count{State: StatePlanned}},
		{steps(StateDropped), Count{State: StatePlanned}},
		{steps(StatePlanned, StatePlanned), Count{Total: 2, State: StatePlanned}},
		{steps(StateDone, StatePlanned), Count{Done: 1, Total: 2, State: StateActive}},
		{steps(StateDone, StateDone+"*", StateDropped), Count{Done: 2, Total: 2, Manual: 1, State: StateDone}},
		// Adding a step to a done item makes it not done: nothing is stored.
		{steps(StateDone, StateDone+"*", StateDropped, StatePlanned), Count{Done: 2, Total: 3, Manual: 1, State: StateActive}},
		{steps(StateDone, StateStopped), Count{Done: 1, Total: 2, Stopped: 1, State: StateStopped}},
		{steps(StateStopped, StatePlanned), Count{Total: 2, Stopped: 1, State: StateActive}},
		{steps(StateActive), Count{Total: 1, State: StateActive}},
	}
	for i, c := range cases {
		if got := CountSteps(c.in); got != c.want {
			t.Errorf("case %d: %+v, want %+v", i, got, c.want)
		}
	}
}

// changingFacts returns a different gate reading on each call, like a
// delivery record that cannot be read and then can (review 145 A4).
type changingFacts struct {
	facts
	readings []Tri
	calls    int
}

func (c *changingFacts) GatePassed(string, string, string) Tri {
	v := c.readings[min(c.calls, len(c.readings)-1)]
	c.calls++
	return v
}

func TestGateIsReadOncePerEvaluation(t *testing.T) {
	gate := `{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"}],"env":"prod"}`
	accepted := `{"task_accepted":"` + taskA + `"}`
	for _, c := range []struct {
		name     string
		doc      string
		readings []Tri
		want     Tri
	}{
		{"unknown_then_false", `{"any":[` + accepted + `,` + gate + `]}`, []Tri{Unknown, False}, Unknown},
		{"unknown_then_true", `{"any":[` + accepted + `,` + gate + `]}`, []Tri{Unknown, True}, Unknown},
		{"gate_first", `{"any":[` + gate + `,` + accepted + `]}`, []Tri{Unknown, False}, Unknown},
		{"nested", `{"all":[` + accepted + `,{"any":[` + accepted + `,` + gate + `]}]}`, []Tri{Unknown, False}, Unknown},
		{"false_then_unknown", `{"any":[` + accepted + `,` + gate + `]}`, []Tri{False, Unknown}, True},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &changingFacts{facts: facts{accepted: map[string]bool{taskA: true}}, readings: c.readings}
			got := mustParse(t, c.doc).Eval(f)
			if got.Value != c.want {
				t.Fatalf("Eval = %v, want %v", got.Value, c.want)
			}
			if f.calls != 1 {
				t.Fatalf("the gate was read %d times in one evaluation, want 1", f.calls)
			}
		})
	}
}

// keyedFacts returns, per gate, a sequence of readings and counts the calls.
type keyedFacts struct {
	facts
	reads map[[3]string][]Tri
	calls map[[3]string]int
}

func (f *keyedFacts) GatePassed(env, test, sha string) Tri {
	k := [3]string{env, test, sha}
	i := f.calls[k]
	f.calls[k]++
	vs := f.reads[k]
	if len(vs) == 0 {
		panic(fmt.Sprintf("unexpected key %v", k))
	}
	return vs[min(i, len(vs)-1)]
}

// Read-once paths from confirmation review 146: a gate repeated in a rule,
// an all that stops early inside an any, a gate list whose first gate is
// false, the three fields of the cache key, and a fresh read on the next
// evaluation.
func TestGateReadOncePaths(t *testing.T) {
	g := `{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"}],"env":"prod"}`
	yes := `{"task_accepted":"` + taskA + `"}`
	no := `{"task_accepted":"` + taskB + `"}`
	m := `{"manual":"note"}`
	key := [3]string{"prod", "T19", spec}
	for _, first := range []Tri{False, True, Unknown} {
		for _, c := range []struct {
			name, doc string
			want      Result
			calls     int
		}{
			{"duplicate_any", `{"any":[` + g + `,` + g + `]}`, Result{Value: first}, 1},
			{"duplicate_all", `{"all":[` + g + `,` + g + `]}`, Result{Value: first}, 1},
			{"nested_duplicate", `{"any":[` + yes + `,{"all":[` + g + `,{"any":[` + no + `,` + g + `]}]}]}`, Result{Value: True}, 1},
			{"skipped_all_in_any", `{"any":[` + yes + `,{"all":[` + no + `,` + g + `,` + g + `]}]}`, Result{Value: True}, 1},
			{"outer_all_false", `{"all":[` + no + `,` + g + `]}`, Result{Value: False}, 0},
			{"manual_cap", `{"any":[` + m + `,` + g + `]}`, Result{Value: True, Manual: first != True}, 1},
		} {
			t.Run(c.name+"/"+first.String(), func(t *testing.T) {
				if first == Unknown && (c.name == "nested_duplicate" || c.name == "skipped_all_in_any" || c.name == "manual_cap") {
					c.want = Result{Value: Unknown}
				}
				f := &keyedFacts{facts: facts{accepted: map[string]bool{taskA: true}, manual: map[string]bool{"note": true}}, reads: map[[3]string][]Tri{key: {first, True, False, Unknown}}, calls: map[[3]string]int{}}
				got := mustParse(t, c.doc).Eval(f)
				if got != c.want || f.calls[key] != c.calls {
					t.Fatalf("got %+v calls %d; want %+v calls %d", got, f.calls[key], c.want, c.calls)
				}
			})
		}
	}
	// A false first gate stops the gate list; the cap must still read T21.
	g2 := `{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"},{"test":"T21","spec_sha256":"` + spec + `"}],"env":"prod"}`
	k2 := [3]string{"prod", "T21", spec}
	f := &keyedFacts{facts: facts{accepted: map[string]bool{taskA: true}}, reads: map[[3]string][]Tri{key: {False, True}, k2: {Unknown, True}}, calls: map[[3]string]int{}}
	if got := mustParse(t, `{"any":[`+yes+`,`+g2+`]}`).Eval(f); got != (Result{Value: Unknown}) || f.calls[key] != 1 || f.calls[k2] != 1 {
		t.Fatalf("gate list cap: %+v %v", got, f.calls)
	}
	// Cache keys must distinguish all three fields.
	gs := []string{g, strings.Replace(g, "prod", "stg", 1), strings.Replace(g, "T19", "T21", 1), strings.Replace(g, spec, strings.Repeat("a", 64), 1)}
	f = &keyedFacts{facts: facts{}, reads: map[[3]string][]Tri{}, calls: map[[3]string]int{}}
	for _, k := range [][3]string{key, {"stg", "T19", spec}, k2, {"prod", "T19", strings.Repeat("a", 64)}} {
		f.reads[k] = []Tri{False, True}
	}
	r := mustParse(t, `{"any":[`+strings.Join(gs, ",")+`]}`)
	if got := r.Eval(f); got.Value != False {
		t.Fatalf("first Eval: %+v", got)
	}
	if got := r.Eval(f); got.Value != True {
		t.Fatalf("second Eval retained cache: %+v", got)
	}
	for k, n := range f.calls {
		if n != 2 {
			t.Fatalf("key %v calls=%d", k, n)
		}
	}
}
