package plan

import (
	"errors"
	"testing"
)

// stepFacts adds the aggregate atoms to facts; tasks records what
// step_tasks asked.
type stepFacts struct {
	facts
	tasks  map[string]Tri // state/min
	landed Tri
	stg    Tri
	asked  []string
}

func (f *stepFacts) StepTasks(state string, min int64) Tri {
	key := state + "/" + string(rune('0'+min))
	f.asked = append(f.asked, key)
	if v, ok := f.tasks[key]; ok {
		return v
	}
	return False
}
func (f *stepFacts) StepLanded() Tri     { return f.landed }
func (f *stepFacts) StepSTGSettled() Tri { return f.stg }

// The aggregate atoms of DESIGN-module-progress D3: closed like every atom.
func TestStepAtomsGrammar(t *testing.T) {
	good := map[string]Tri{
		`{"step_tasks":{"state":"accepted"}}`:                                      True,
		`{"step_tasks":{"state":"accepted","min":2}}`:                              False,
		`{"step_tasks":{"min":3,"state":"accepted"}}`:                              Unknown,
		`{"step_landed":{}}`:                                                       True,
		`{"step_stg_settled":{}}`:                                                  Unknown,
		`{"all":[{"step_landed":{}},{"step_stg_settled":{}}]}`:                     Unknown,
		`{"any":[{"step_tasks":{"state":"accepted","min":2}},{"step_landed":{}}]}`: True,
	}
	f := &stepFacts{tasks: map[string]Tri{"accepted/1": True, "accepted/3": Unknown}, landed: True, stg: Unknown}
	for doc, want := range good {
		r := mustParse(t, doc)
		if got := r.Eval(f).Value; got != want {
			t.Errorf("Eval(%s) = %s, want %s", doc, got, want)
		}
		if r.Standing() || r.OnlyStanding() || len(r.Refs()) != 0 || len(r.Watches()) != 0 || len(r.Gates()) != 0 || len(r.BoundRefs()) != 0 {
			t.Errorf("%s: names an outside record or a standing condition", doc)
		}
	}
	bad := []string{
		`{"step_tasks":{}}`,
		`{"step_tasks":"accepted"}`,
		`{"step_tasks":{"state":"done"}}`,
		`{"step_tasks":{"state":"proposed"}}`,
		`{"step_tasks":{"state":"submitted","min":1}}`,
		`{"any":[{"step_landed":{}},{"step_tasks":{"state":"ready"}}]}`,
		`{"all":[{"step_tasks":{"state":"accepted"}},{"any":[{"step_tasks":{"state":"verifying"}}]}]}`,
		`{"step_tasks":{"state":"Accepted"}}`,
		`{"step_tasks":{"state":"accepted","min":0}}`,
		`{"step_tasks":{"state":"accepted","min":-1}}`,
		`{"step_tasks":{"state":"accepted","min":1.5}}`,
		`{"step_tasks":{"state":"accepted","min":"2"}}`,
		`{"step_tasks":{"state":"accepted","max":2}}`,
		`{"step_tasks":{"min":2}}`,
		`{"step_tasks":{"state":"accepted","min":1,"of":"implement"}}`,
		`{"step_tasks":{"state":"accepted"},"step_landed":{}}`,
		`{"step_landed":{"env":"prod"}}`,
		`{"step_landed":true}`,
		`{"step_landed":[]}`,
		`{"step_stg_settled":null}`,
		`{"step_stg_settled":{"batch":"b1"}}`,
		`{"step_deployed":{"env":"prod"}}`,
	}
	for _, doc := range bad {
		if _, err := Parse([]byte(doc)); !errors.Is(err, ErrRule) {
			t.Errorf("Parse(%s) err = %v, want ErrRule", doc, err)
		}
	}
	// Every other state a task row may hold (agentrt.task's CHECK) is
	// refused: most are reached by proposing a task, not by its acceptance
	// (review 630).
	for _, state := range []string{"proposed", "awaiting_owner", "ready", "leased", "running", "submitted", "verifying",
		"rework", "rejected", "blocked", "paused", "cancelled"} {
		if _, err := Parse([]byte(`{"step_tasks":{"state":"` + state + `"}}`)); !errors.Is(err, ErrRule) {
			t.Errorf("step_tasks of %s: err = %v, want ErrRule", state, err)
		}
	}
	mustParse(t, `{"step_tasks":{"state":"`+StepTaskState+`"}}`)
}

// step_tasks passes its state and min (1 when left out) to the reader.
func TestStepTasksAsksStateAndMin(t *testing.T) {
	f := &stepFacts{}
	mustParse(t, `{"all":[{"step_tasks":{"state":"accepted"}},{"step_tasks":{"state":"accepted","min":3}}]}`).Eval(f)
	if len(f.asked) != 1 || f.asked[0] != "accepted/1" {
		// all stops at the first false.
		t.Fatalf("asked %v", f.asked)
	}
	f = &stepFacts{tasks: map[string]Tri{"accepted/1": True}}
	mustParse(t, `{"all":[{"step_tasks":{"state":"accepted"}},{"step_tasks":{"state":"accepted","min":3}}]}`).Eval(f)
	if len(f.asked) != 2 || f.asked[1] != "accepted/3" {
		t.Fatalf("asked %v", f.asked)
	}
}

// Each atom reads true, false and unknown, combined by Kleene logic; an
// unknown aggregate does not cap the rule the way an unreadable gate does
// (it is a step on its way, not a delivery record that cannot be read).
func TestStepAtomsThreeValued(t *testing.T) {
	for _, v := range []Tri{True, False, Unknown} {
		f := &stepFacts{tasks: map[string]Tri{"accepted/2": v}, landed: v, stg: v}
		for _, doc := range []string{`{"step_tasks":{"state":"accepted","min":2}}`, `{"step_landed":{}}`, `{"step_stg_settled":{}}`} {
			r := mustParse(t, doc)
			if got := r.Eval(f).Value; got != v {
				t.Errorf("%s with %s = %s", doc, v, got)
			}
			if r.Eval(f).Manual {
				t.Errorf("%s holds through a manual atom", doc)
			}
		}
	}
	f := &stepFacts{facts: facts{signoff: map[string]bool{"TD-1": true}}, landed: True, stg: Unknown}
	cases := map[string]Tri{
		`{"all":[{"step_landed":{}},{"step_stg_settled":{}}]}`:                     Unknown,
		`{"any":[{"step_stg_settled":{}},{"owner_signoff":"TD-1"}]}`:               True,
		`{"any":[{"step_stg_settled":{}},{"owner_signoff":"TD-2"}]}`:               Unknown,
		`{"all":[{"step_stg_settled":{}},{"owner_signoff":"TD-2"}]}`:               False,
		`{"all":[{"step_landed":{}},{"step_tasks":{"state":"accepted"}}]}`:         False,
		`{"any":[{"step_landed":{}},{"step_tasks":{"state":"accepted","min":4}}]}`: True,
	}
	for doc, want := range cases {
		if got := mustParse(t, doc).Eval(f).Value; got != want {
			t.Errorf("Eval(%s) = %s, want %s", doc, got, want)
		}
	}
}

// A facts reader that does not answer the aggregate atoms reads them
// unknown: they never make a step done there.
func TestStepAtomsFailClosed(t *testing.T) {
	for _, doc := range []string{`{"step_tasks":{"state":"accepted"}}`, `{"step_landed":{}}`, `{"step_stg_settled":{}}`,
		`{"any":[{"step_landed":{}},{"task_accepted":"` + taskB + `"}]}`} {
		if got := mustParse(t, doc).Eval(facts{}).Value; got != Unknown {
			t.Errorf("Eval(%s) without StepFacts = %s, want unknown", doc, got)
		}
	}
}

// Names finds each aggregate atom anywhere, so standingRule's list of
// refused atoms sees through them and finds none of its own.
func TestStepAtomsNames(t *testing.T) {
	r := mustParse(t, `{"all":[{"step_tasks":{"state":"accepted"}},{"any":[{"step_landed":{}},{"step_stg_settled":{}}]}]}`)
	for _, atom := range []string{AtomStepTasks, AtomStepLanded, AtomStepSTG} {
		if !r.Names(atom) {
			t.Errorf("Names(%s) = false", atom)
		}
	}
	for _, atom := range []string{AtomManual, AtomCI, AtomGates, AtomApproval, AtomQuestion, AtomTask, AtomSignoff, AtomMain} {
		if r.Names(atom) {
			t.Errorf("Names(%s) = true", atom)
		}
	}
}
