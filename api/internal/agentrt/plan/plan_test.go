package plan

import (
	"errors"
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
