package s5check

import (
	"math"
	"testing"
)

func TestCanonicalUTF16AndNumericKeyParity(t *testing.T) {
	input := map[string]any{"2": 2, "10": 10, "\uE000": "private", "😀": "astral", "array": []int{3, 2, 1}}
	want := "{\"10\":10,\"2\":2,\"array\":[3,2,1],\"😀\":\"astral\",\"\uE000\":\"private\"}"
	raw, err := CanonicalJSON(input)
	if err != nil || string(raw) != want {
		t.Fatalf("cross-language canonical order: %s, %v", raw, err)
	}
	for _, value := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		if _, err := CanonicalJSON(value); err == nil {
			t.Fatal("non-finite value entered a canonical digest")
		}
	}
	raw, err = CanonicalJSON(math.Copysign(0, -1))
	if err != nil || string(raw) != "0" {
		t.Fatalf("negative zero canonical form: %s, %v", raw, err)
	}
}
