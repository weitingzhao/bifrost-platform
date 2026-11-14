package store_test

import (
	"math"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestWilson(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		k, n int
		want float64
	}{{0, 0, 0}, {0, 5, 0}, {5, 5, 0.566}, {1, 3, 0.0615}, {90, 100, 0.8256}} {
		if got := store.Wilson(c.k, c.n); math.Abs(got-c.want) > 0.001 {
			t.Errorf("Wilson(%d, %d) = %.4f, want %.4f", c.k, c.n, got, c.want)
		}
	}
}
