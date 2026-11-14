package landing

import (
	"fmt"
	"slices"
	"testing"
)

// Split halves a batch, the first half the larger.
func TestBatchSplit(t *testing.T) {
	t.Parallel()
	for n, want := range map[int][2]int{2: {1, 1}, 3: {2, 1}, 8: {4, 4}, 5: {3, 2}} {
		var m []string
		for i := range n {
			m = append(m, fmt.Sprint(i))
		}
		a, b := Split(m)
		if len(a) != want[0] || len(b) != want[1] || !slices.Equal(append(a, b...), m) {
			t.Fatal(n, a, b)
		}
	}
}
