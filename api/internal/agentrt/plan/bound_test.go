package plan

import (
	"fmt"
	"testing"
)

type boundFacts struct {
	facts
	value Tri
}

func (f boundFacts) BoundFact(atom, id string, version int) Tri {
	if id != "acceptance" || version != 2 {
		return Unknown
	}
	return f.value
}
func TestVersionedCompletionAtoms(t *testing.T) {
	for _, atom := range []string{AtomMain, AtomDeployment, AtomReview, AtomExternal} {
		r := mustParse(t, fmt.Sprintf(`{"%s":{"binding":"acceptance","version":2}}`, atom))
		if !r.Standing() || r.OnlyStanding() || !r.Names(atom) {
			t.Fatalf("wrong standing semantics: %s", atom)
		}
		for _, v := range []Tri{False, True, Unknown} {
			if got := r.Eval(boundFacts{value: v}); got.Value != v || got.Manual {
				t.Fatalf("%s: %+v", atom, got)
			}
		}
		if r.Eval(facts{}).Value != Unknown {
			t.Fatal("legacy reader granted completion")
		}
		for _, bad := range []string{`"x"`, `{"binding":"x"}`, `{"binding":"x","version":0}`, `{"binding":"x","version":1.5}`, `{"binding":"x","version":1,"pass":true}`, `{"binding":"","version":1}`, `{"binding":"x","version":"1"}`} {
			if _, err := Parse([]byte(fmt.Sprintf(`{"%s":%s}`, atom, bad))); err == nil {
				t.Fatalf("accepted %s", bad)
			}
		}
	}
}

func TestStaleBoundFactCannotBeHiddenByAny(t *testing.T) {
	r := mustParse(t, `{"any":[{"owner_signoff":"old"},{"main_effective":{"binding":"acceptance","version":2}}]}`)
	f := boundFacts{facts: facts{signoff: map[string]bool{"old": true}}, value: Unknown}
	if r.Eval(f).Value != Unknown {
		t.Fatal("stale current condition hidden by old evidence")
	}
}
