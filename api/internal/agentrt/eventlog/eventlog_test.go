package eventlog

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// emptyVocabulary leaves no vocabulary declared for the length of a test and
// puts back what was there. The tests that use it do not run in parallel.
func emptyVocabulary(t *testing.T) {
	t.Helper()
	vocabularyMu.Lock()
	d, ty, bo, va, wh, be, af, ev := declared, types, bodies, values, wholeValues, before, after, events
	declared, types, bodies, values = false, map[string]bool{}, map[string][]Field{}, map[string]map[string]Value{}
	wholeValues, before, after, events = map[string]map[string]func(any) string{}, map[string]func(map[string]any) error{}, map[string]func(map[string]any) error{}, nil
	vocabularyMu.Unlock()
	t.Cleanup(func() {
		vocabularyMu.Lock()
		declared, types, bodies, values, wholeValues, before, after, events = d, ty, bo, va, wh, be, af, ev
		vocabularyMu.Unlock()
	})
}

// 2026-11-14, when the event log left the store: the vocabulary is declared
// by other packages' inits, so a binary that links this package alone has
// none. It must refuse to append, not append unchecked. The transaction is
// nil on purpose: every refusal here comes before it is used.
func TestAppendRefusesWithoutAVocabulary(t *testing.T) {
	emptyVocabulary(t)
	_, err := Append(context.Background(), nil, Event{Stream: "s", Type: "note", Actor: "a"})
	if err == nil || !strings.Contains(err.Error(), "no vocabulary is declared") {
		t.Fatalf("an event with no vocabulary declared: %v", err)
	}
}

// The positive control of the test above, and the order of the checks: with
// a vocabulary declared, what refuses is the declared check, asked about the
// event given; the checks of every event come before the body's.
func TestAppendAsksTheDeclaredVocabulary(t *testing.T) {
	emptyVocabulary(t)
	refused := errors.New("refused by the domain")
	var asked Event
	Declare(Vocabulary{
		Bodies: map[string][]Field{"thing_done": {{"thing_id", ID, false}, {"why", Str, true}}},
		Events: []func(Event) error{func(ev Event) error {
			asked = ev
			if ev.Stream == "closed" {
				return refused
			}
			return nil
		}},
	})
	if _, err := Append(context.Background(), nil, Event{Stream: "closed", Type: "thing_done", Actor: "a"}); !errors.Is(err, refused) || asked.Stream != "closed" {
		t.Fatalf("the declared check of every event was not what refused: %v, asked %+v", err, asked)
	}
	if _, err := Append(context.Background(), nil, Event{Stream: "s", Type: "thing_done", Actor: "a"}); err == nil || !strings.Contains(err.Error(), `needs "thing_id"`) {
		t.Fatalf("a body without its required field: %v", err)
	}
	if _, err := Append(context.Background(), nil, Event{Stream: "s", Type: "other_thing", Actor: "a"}); err == nil || !strings.Contains(err.Error(), "has no body defined yet") {
		t.Fatalf("a type nobody declared: %v", err)
	}
}

// Two domains declare apart and the vocabulary is their sum (2026-11-14: the
// reason the vocabulary left the store is that a domain package brings its
// own). One type's body declared twice stops the process: the second would
// silently replace the first.
func TestDomainsDeclareApart(t *testing.T) {
	emptyVocabulary(t)
	Declare(Vocabulary{Types: []string{"a_seen", "a_retired"}, Bodies: map[string][]Field{"a_seen": {{"n", Int, false}}},
		Values: map[string]map[string]Value{"a_seen": {}}})
	Declare(Vocabulary{Bodies: map[string][]Field{"b_seen": {{"kind", Str, false}}},
		Values: map[string]map[string]Value{"b_seen": {"kind": OneOf("x", "y")}},
		After: map[string]func(map[string]any) error{"b_seen": func(b map[string]any) error {
			if b["kind"] == "y" {
				return Invalid("y is not taken today")
			}
			return nil
		}}})
	all, written := EventTypes()
	if strings.Join(all, " ") != "a_retired a_seen b_seen" || strings.Join(written, " ") != "a_seen b_seen" {
		t.Fatalf("the sum of two declarations: %v, with a body %v", all, written)
	}
	if err := CheckBody("a_seen", map[string]any{"n": 1}); err != nil {
		t.Fatalf("the first domain's type: %v", err)
	}
	if err := CheckBody("b_seen", map[string]any{"kind": "x"}); err != nil {
		t.Fatalf("the second domain's type: %v", err)
	}
	if err := CheckBody("b_seen", map[string]any{"kind": "z"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a value outside the second domain's enum: %v", err)
	}
	if err := CheckBody("b_seen", map[string]any{"kind": "y"}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "not taken today") {
		t.Fatalf("the second domain's own check: %v", err)
	}
	if !IsEventType("a_retired") || IsEventType("c_seen") {
		t.Fatal("a type with no body is in the vocabulary; one nobody declared is not")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a body declared twice did not stop the process")
		}
	}()
	Declare(Vocabulary{Bodies: map[string][]Field{"a_seen": {{"m", Int, false}}}})
}
