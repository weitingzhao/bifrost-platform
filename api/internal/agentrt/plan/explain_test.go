package plan

import (
	"reflect"
	"strings"
	"testing"
)

// explainLeaves are the atoms Explain covers, two step_tasks among them so
// a rule can name the same atom with different arguments.
var explainLeaves = []string{
	`{"step_tasks":{"state":"accepted"}}`,
	`{"step_tasks":{"state":"accepted","min":2}}`,
	`{"step_landed":{}}`,
	`{"step_stg_settled":{}}`,
}

// explainRules builds every rule of all and any over the leaves: one level
// with one to three children, and two levels with two children each drawn
// from the leaves and the one-level rules of up to two children.
func explainRules() []string {
	var one, small []string
	for _, op := range []string{"all", "any"} {
		for _, a := range explainLeaves {
			one = append(one, `{"`+op+`":[`+a+`]}`)
			for _, b := range explainLeaves {
				pair := `{"` + op + `":[` + a + `,` + b + `]}`
				one, small = append(one, pair), append(small, pair)
				for _, c := range explainLeaves {
					one = append(one, `{"`+op+`":[`+a+`,`+b+`,`+c+`]}`)
				}
			}
		}
	}
	out := append(append([]string{}, explainLeaves...), one...)
	kids := append(append([]string{}, explainLeaves...), small...)
	for _, op := range []string{"all", "any"} {
		for _, a := range kids {
			for _, b := range kids {
				out = append(out, `{"`+op+`":[`+a+`,`+b+`]}`)
			}
		}
	}
	return out
}

// countingFacts answers the aggregate atoms and counts each read.
type countingFacts struct {
	facts
	t1, t2, landed, stg Tri
	reads               map[string]int
}

func (f *countingFacts) StepTasks(state string, min int64) Tri {
	f.reads["tasks/"+string(rune('0'+min))]++
	if min == 2 {
		return f.t2
	}
	return f.t1
}
func (f *countingFacts) StepLanded() Tri     { f.reads["landed"]++; return f.landed }
func (f *countingFacts) StepSTGSettled() Tri { f.reads["stg"]++; return f.stg }

// Over every rule Explain covers and every reading of its atoms, Explain's
// value is Eval's whole Result: the value, and Manual false (none of the
// three is manual).
func TestExplainEqualsEval(t *testing.T) {
	tris := []Tri{True, False, Unknown}
	rules := explainRules()
	if len(rules) < 2700 {
		t.Fatalf("only %d rules", len(rules))
	}
	for _, doc := range rules {
		r := mustParse(t, doc)
		for _, t1 := range tris {
			for _, t2 := range tris {
				for _, l := range tris {
					for _, s := range tris {
						f := &countingFacts{t1: t1, t2: t2, landed: l, stg: s, reads: map[string]int{}}
						want := r.Eval(f)
						n, ok, un := r.Explain(f)
						if !ok || un != nil {
							t.Fatalf("Explain(%s) not supported: %v", doc, un)
						}
						if n.Value != want.Value || want.Manual {
							t.Fatalf("Explain(%s) with %s/%s/%s/%s = %s, Eval = %+v", doc, t1, t2, l, s, n.Value, want)
						}
						if (len(n.Blocking()) == 0) != (n.Value == True) {
							t.Fatalf("Explain(%s) = %s with blocking %+v", doc, n.Value, n.Blocking())
						}
					}
				}
			}
		}
	}
}

// Explain does not stop early: every atom the rule names is read, also
// after an all has gone false (where Eval stops).
func TestExplainReadsEveryAtom(t *testing.T) {
	doc := `{"all":[{"step_landed":{}},{"any":[{"step_tasks":{"state":"accepted"}},{"step_stg_settled":{}}]},{"step_tasks":{"state":"accepted","min":2}}]}`
	f := &countingFacts{t1: False, t2: True, landed: False, stg: Unknown, reads: map[string]int{}}
	n, ok, _ := mustParse(t, doc).Explain(f)
	if !ok || n.Value != False {
		t.Fatalf("Explain = %+v, %v", n, ok)
	}
	want := map[string]int{"landed": 1, "tasks/1": 1, "stg": 1, "tasks/2": 1}
	if !reflect.DeepEqual(f.reads, want) {
		t.Fatalf("reads %v, want %v", f.reads, want)
	}
	f.reads = map[string]int{}
	mustParse(t, doc).Eval(f)
	if !reflect.DeepEqual(f.reads, map[string]int{"landed": 1}) {
		t.Fatalf("Eval reads %v (the test no longer shows the difference)", f.reads)
	}
}

// A facts reader without the aggregate atoms reads them unknown, as Eval.
func TestExplainWithoutStepFacts(t *testing.T) {
	for _, doc := range explainLeaves {
		n, ok, _ := mustParse(t, doc).Explain(facts{})
		if !ok || n.Value != Unknown || n.Value != mustParse(t, doc).Eval(facts{}).Value {
			t.Fatalf("Explain(%s) without StepFacts = %+v", doc, n)
		}
	}
}

// Any other atom, anywhere in the rule, is named and nothing is read.
func TestExplainNamesWhatItDoesNotCover(t *testing.T) {
	cases := map[string][]string{
		`{"owner_signoff":"TD-1"}`: {"owner_signoff"},
		`{"all":[{"step_landed":{}},{"any":[{"manual":"x"},{"owner_signoff":"TD-1"},{"manual":"y"}]}]}`: {"manual", "owner_signoff"},
		`{"any":[{"step_landed":{}},{"main_effective":{"binding":"b1","version":1}}]}`:                  {"main_effective"},
		`{"all":[{"step_stg_settled":{}},{"task_accepted":"` + taskB + `"}]}`:                           {"task_accepted"},
	}
	for doc, want := range cases {
		f := &countingFacts{reads: map[string]int{}}
		n, ok, un := mustParse(t, doc).Explain(f)
		if ok || !reflect.DeepEqual(un, want) || len(f.reads) != 0 || n.Kind != "" {
			t.Errorf("Explain(%s) = %v, %v, reads %v; want unsupported %v", doc, ok, un, f.reads, want)
		}
	}
	if _, ok, un := (Rule{}).Explain(&countingFacts{}); ok || un != nil {
		t.Errorf("zero rule: %v %v", ok, un)
	}
}

// What blocks: an all's children that are not true, in rule order and
// flattened through nested alls; an any's branches as one OneOf; nothing
// under a true any. The same atom named twice has two places.
func TestExplainBlocking(t *testing.T) {
	f := &countingFacts{t1: False, t2: Unknown, landed: True, stg: False, reads: map[string]int{}}
	var show func([]Blocking) string
	show = func(bs []Blocking) string {
		var p []string
		for _, b := range bs {
			if b.Atom != nil {
				p = append(p, b.Atom.Kind+"@"+fmtPath(b.Atom.Path))
				continue
			}
			var br []string
			for _, x := range b.OneOf {
				br = append(br, "("+show(x)+")")
			}
			p = append(p, "one_of"+strings.Join(br, "|"))
		}
		return strings.Join(p, " ")
	}
	cases := map[string]string{
		`{"all":[{"step_tasks":{"state":"accepted"}},{"step_landed":{}},{"step_stg_settled":{}}]}`:                            "step_tasks@0 step_stg_settled@2",
		`{"any":[{"step_landed":{}},{"step_stg_settled":{}}]}`:                                                                "",
		`{"any":[{"step_tasks":{"state":"accepted"}},{"step_stg_settled":{}}]}`:                                               "one_of(step_tasks@0)|(step_stg_settled@1)",
		`{"all":[{"all":[{"step_stg_settled":{}},{"step_landed":{}}]},{"step_tasks":{"state":"accepted","min":2}}]}`:          "step_stg_settled@0.0 step_tasks@1",
		`{"all":[{"any":[{"step_stg_settled":{}},{"all":[{"step_landed":{}},{"step_stg_settled":{}}]}]},{"step_landed":{}}]}`: "one_of(step_stg_settled@0.0)|(step_stg_settled@0.1.1)",
	}
	for doc, want := range cases {
		n, ok, _ := mustParse(t, doc).Explain(f)
		if !ok {
			t.Fatalf("Explain(%s) not supported", doc)
		}
		if got := show(n.Blocking()); got != want {
			t.Errorf("Blocking(%s) = %q, want %q", doc, got, want)
		}
	}
	n, _, _ := mustParse(t, `{"all":[{"step_tasks":{"state":"accepted","min":2}},{"step_tasks":{"state":"accepted"}}]}`).Explain(f)
	bs := n.Blocking()
	if len(bs) != 2 || bs[0].Atom.Min != 2 || bs[1].Atom.Min != 1 || bs[0].Atom.State != StepTaskState ||
		reflect.DeepEqual(bs[0].Atom.Path, bs[1].Atom.Path) {
		t.Fatalf("two step_tasks: %+v", bs)
	}
}

func fmtPath(p []int) string {
	s := make([]string, len(p))
	for i, x := range p {
		s[i] = string(rune('0' + x))
	}
	return strings.Join(s, ".")
}
