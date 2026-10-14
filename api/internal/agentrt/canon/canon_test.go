package canon

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// golden.jsonl is written by testdata/gen_golden.py with the recorder's own
// json.dumps call; CI has no Python, so the fixture is committed.
func TestMatchesTheRecorder(t *testing.T) {
	f, err := os.Open("testdata/golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var c struct {
			Input     string `json:"input"`
			Canonical string `json:"canonical"`
			Sha256    string `json:"sha256"`
		}
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		got, err := Marshal([]byte(c.Input))
		if err != nil {
			t.Fatalf("case %d: %v", n, err)
		}
		if string(got) != c.Canonical {
			t.Fatalf("case %d:\n got %s\nwant %s", n, got, c.Canonical)
		}
		h := sha256.Sum256(got)
		if hex.EncodeToString(h[:]) != c.Sha256 {
			t.Fatalf("case %d: sha256 differs", n)
		}
		v, _ := Parse([]byte(c.Input))
		if sum, err := Sum(v); err != nil || sum != c.Sha256 {
			t.Fatalf("case %d: Sum = %s, %v", n, sum, err)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 13 {
		t.Fatalf("read %d golden cases, want at least 13", n)
	}
}

func TestRefusesWhatTheTwoSidesWouldEncodeDifferently(t *testing.T) {
	cases := []struct {
		doc  string
		want error
	}{
		{`{"x":1.5}`, ErrFloat},
		{`{"x":1.0}`, ErrFloat},
		{`{"x":1e3}`, ErrFloat},
		{`{"x":9223372036854775808}`, ErrFloat},
		{`{"a":1,"a":2}`, ErrDuplicate},
		{"{\"a\":\"\xff\"}", ErrUTF8},
		{`{"a":1} {}`, ErrTrailing},
	}
	for _, c := range cases {
		if _, err := Marshal([]byte(c.doc)); !errors.Is(err, c.want) {
			t.Errorf("%q: err = %v, want %v", c.doc, err, c.want)
		}
	}
	if _, err := Encode(map[string]any{"x": 0.5}); !errors.Is(err, ErrFloat) {
		t.Errorf("float64 value: err = %v", err)
	}
	if _, err := Encode(map[string]any{"bad\xff": 1}); !errors.Is(err, ErrUTF8) {
		t.Errorf("invalid key: err = %v", err)
	}
}

func TestEncodeAcceptsGoValues(t *testing.T) {
	got, err := Encode(map[string]any{
		"b": []string{"<", "\u2028"},
		"a": int64(-3),
		"c": json.Number("42"),
		"d": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":-3,\"b\":[\"<\",\"\u2028\"],\"c\":42,\"d\":null}"
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
