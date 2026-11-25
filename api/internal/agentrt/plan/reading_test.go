package plan

import (
	"errors"
	"testing"
)

// Origin: 2026-11-25, the map line asked the plan (map/done-rules-for-plan-2026-11-25.md):
// R6:6.9's eleven steps have empty rules because no atom could read a number the
// platform keeps; the reading atom compares one with a threshold.

type readingFacts struct {
	facts
	docs map[string]string
	read map[string]int
}

func (f *readingFacts) ReadingDoc(source string) ([]byte, bool) {
	if f.read == nil {
		f.read = map[string]int{}
	}
	f.read[source]++
	d, ok := f.docs[source]
	return []byte(d), ok
}

const statusDoc = `{"inventory":[{"repo":"a","gap":0,"measured":true},{"repo":"b","gap":0,"measured":true}],
 "daily":[{"reads":{"status":3}},{"reads":{"status":0}}],"pool":{"oldest_days":9}}`

func TestReadingParses(t *testing.T) {
	for _, bad := range []string{
		`{"reading":{"source":"catalog.status","path":"pool.oldest_days","op":"<="}}`,
		`{"reading":{"source":"Catalog","path":"pool","op":"<=","value":1}}`,
		`{"reading":{"source":"catalog.status","path":"pool..x","op":"<=","value":1}}`,
		`{"reading":{"source":"catalog.status","path":"pool","op":"~","value":1}}`,
		`{"reading":{"source":"catalog.status","path":"pool","op":"<","value":true}}`,
		`{"reading":{"source":"catalog.status","path":"pool","op":"==","value":"0"}}`,
	} {
		if _, err := Parse([]byte(bad)); !errors.Is(err, ErrRule) {
			t.Errorf("%s: want ErrRule, got %v", bad, err)
		}
	}
	r, err := Parse([]byte(`{"all":[{"reading":{"source":"catalog.status","path":"inventory.*.gap","op":"==","value":0}},
	  {"reading":{"source":"catalog.status","path":"inventory.*.measured","op":"==","value":true}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Readings(); len(got) != 1 || got[0] != "catalog.status" {
		t.Fatalf("Readings = %v", got)
	}
	if r.Standing() || !r.Names(AtomReading) {
		t.Fatal("a reading is history and is named")
	}
}

func TestReadingEvaluates(t *testing.T) {
	f := &readingFacts{docs: map[string]string{"catalog.status": statusDoc}}
	for rule, want := range map[string]Tri{
		// every element must hold, and the path must reach one
		`{"reading":{"source":"catalog.status","path":"inventory.*.gap","op":"==","value":0}}`:         True,
		`{"reading":{"source":"catalog.status","path":"inventory.*.measured","op":"==","value":true}}`: True,
		`{"reading":{"source":"catalog.status","path":"daily.*.reads.status","op":">","value":0}}`:     False,
		`{"reading":{"source":"catalog.status","path":"pool.oldest_days","op":"<=","value":14}}`:       True,
		`{"reading":{"source":"catalog.status","path":"pool.oldest_days","op":"<","value":9}}`:         False,
		// not there, not a number, or no source: unknown, never done
		`{"reading":{"source":"catalog.status","path":"pool.missing","op":"==","value":0}}`:            Unknown,
		`{"reading":{"source":"catalog.status","path":"inventory.*.repo","op":"==","value":0}}`:        Unknown,
		`{"reading":{"source":"catalog.status","path":"inventory","op":"==","value":0}}`:               Unknown,
		`{"reading":{"source":"other.doc","path":"pool.oldest_days","op":"<=","value":14}}`:            Unknown,
		`{"reading":{"source":"catalog.status","path":"inventory.*.gap","op":"!=","value":0}}`:         False,
		`{"reading":{"source":"catalog.status","path":"inventory.*.measured","op":"!=","value":true}}`: False,
	} {
		r, err := Parse([]byte(rule))
		if err != nil {
			t.Fatalf("%s: %v", rule, err)
		}
		if got := r.Eval(f).Value; got != want {
			t.Errorf("%s: got %v want %v", rule, got, want)
		}
	}
	// A facts reader without ReadingFacts reads every reading unknown.
	r, _ := Parse([]byte(`{"reading":{"source":"catalog.status","path":"pool.oldest_days","op":"<=","value":14}}`))
	if got := r.Eval(facts{}).Value; got != Unknown {
		t.Fatalf("plain facts: got %v", got)
	}
}

func TestReadingReadsEachSourceOncePerEvaluation(t *testing.T) {
	f := &readingFacts{docs: map[string]string{"catalog.status": statusDoc}}
	r, _ := Parse([]byte(`{"all":[{"reading":{"source":"catalog.status","path":"inventory.*.gap","op":"==","value":0}},
	  {"reading":{"source":"catalog.status","path":"pool.oldest_days","op":"<=","value":14}}]}`))
	if got := r.Eval(f).Value; got != True {
		t.Fatalf("got %v", got)
	}
	if f.read["catalog.status"] != 1 {
		t.Fatalf("read %d times, want once", f.read["catalog.status"])
	}
}
