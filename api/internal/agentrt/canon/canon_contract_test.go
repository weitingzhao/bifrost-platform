package canon_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Origin: 2026-11-16, the review of the foundation code found the base's
// core packages tested from the inside only; the Owner's rule the same day is
// that a domain is judged against its contract. These are canon's promises
// to the packages that hash with it (the ledger, evidence, the actors), from
// outside the package: what the package comment says, held.

// The same document gives the same bytes whatever order its keys came in,
// compact, keys sorted, and without the escapes encoding/json would add.
func TestOneDocumentHasOneEncoding(t *testing.T) {
	a, err := canon.Marshal([]byte(`{"b": 1, "a": {"y": [true, null], "x": "<&> é"}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canon.Marshal([]byte(`{"a":{"x":"<&> é","y":[true,null]},"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":{\"x\":\"<&> é\",\"y\":[true,null]},\"b\":1}"
	if string(a) != want || string(b) != want {
		t.Fatalf("encodings:\n %s\n %s\nwant\n %s", a, b, want)
	}
	// A value built in Go encodes to the same bytes as the document.
	built, err := canon.Encode(map[string]any{"b": 1, "a": map[string]any{"y": []any{true, nil}, "x": "<&> é"}})
	if err != nil || string(built) != want {
		t.Fatalf("Encode = %s, %v", built, err)
	}
	// Encoding what was encoded changes nothing.
	again, err := canon.Marshal(a)
	if err != nil || string(again) != want {
		t.Fatalf("a second pass changed the bytes: %s, %v", again, err)
	}
	// The positive control: a different document has different bytes.
	other, _ := canon.Marshal([]byte(`{"a":{"x":"<&> é","y":[true,null]},"b":2}`))
	if string(other) == want {
		t.Fatal("two documents encoded alike")
	}
}

// Sum is the sha256 of exactly those bytes, in lowercase hex.
func TestSumIsTheHashOfTheEncoding(t *testing.T) {
	v := map[string]any{"k": "v", "n": 7}
	enc, err := canon.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(enc)
	sum, err := canon.Sum(v)
	if err != nil || sum != hex.EncodeToString(h[:]) {
		t.Fatalf("Sum = %s, %v; want the hash of %s", sum, err, enc)
	}
}

// What the two sides would encode differently is an error, each with its
// own, never a silent rewrite; and a large integer is kept exactly.
func TestWhatCannotBeAgreedIsRefused(t *testing.T) {
	for name, c := range map[string]struct {
		doc  string
		want error
	}{
		"a float":         {`{"n": 1.5}`, canon.ErrFloat},
		"an exponent":     {`{"n": 1e3}`, canon.ErrFloat},
		"a duplicate key": {`{"a": 1, "a": 2}`, canon.ErrDuplicate},
		"trailing data":   {`{"a": 1} {"b": 2}`, canon.ErrTrailing},
		"invalid UTF-8":   {"{\"a\": \"\xff\"}", canon.ErrUTF8},
	} {
		if _, err := canon.Marshal([]byte(c.doc)); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	if _, err := canon.Encode(map[string]any{"n": 1.5}); !errors.Is(err, canon.ErrFloat) {
		t.Errorf("Encode of a Go float: %v", err)
	}
	big, err := canon.Marshal([]byte(`{"n": 9007199254740993}`))
	if err != nil || string(big) != `{"n":9007199254740993}` {
		t.Fatalf("a large integer: %s, %v", big, err)
	}
}
