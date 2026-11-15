package eventlog

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 2026-11-14, when the event log left the store: the vocabulary check is
// registered by another package's init, so a binary that links this package
// alone has none. It must refuse to append, not append unchecked. The
// transaction is nil on purpose: both refusals come before it is used.
func TestAppendRefusesWithoutAVocabulary(t *testing.T) {
	saved := vocabulary.Swap(nil)
	defer vocabulary.Store(saved)
	_, err := Append(context.Background(), nil, Event{Stream: "s", Type: "note", Actor: "a"})
	if err == nil || !strings.Contains(err.Error(), "no vocabulary is registered") {
		t.Fatalf("an event with no vocabulary registered: %v", err)
	}
}

// The positive control of the test above: with a vocabulary registered, its
// refusal is what Append returns, and it is asked about the event given.
func TestAppendAsksTheRegisteredVocabulary(t *testing.T) {
	saved := vocabulary.Load()
	defer vocabulary.Store(saved)
	refused := errors.New("refused by the vocabulary")
	var asked Event
	Register(func(ev Event) error { asked = ev; return refused })
	_, err := Append(context.Background(), nil, Event{Stream: "s", Type: "note", Actor: "a"})
	if !errors.Is(err, refused) || asked.Stream != "s" || asked.Body == nil {
		t.Fatalf("the registered vocabulary was not what refused: %v, asked %+v", err, asked)
	}
}
