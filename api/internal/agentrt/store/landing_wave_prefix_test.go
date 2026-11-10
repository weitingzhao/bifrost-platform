package store

import (
	"math/bits"
	"testing"
)

func TestWavePrefixSearch(t *testing.T) {
	t.Parallel()
	for n := 1; n <= 128; n++ {
		for want := 1; want <= n; want++ {
			s := WavePrefixSearch{Red: n}
			runs := 0
			for {
				prefix, culprit, err := s.Next()
				if err != nil {
					t.Fatal(err)
				}
				if culprit != 0 {
					if culprit != want {
						t.Fatalf("n=%d: culprit=%d, want %d", n, culprit, want)
					}
					break
				}
				if prefix <= s.Green || prefix >= s.Red {
					t.Fatalf("probe %d outside bounds %+v", prefix, s)
				}
				s, err = s.Observe(prefix, prefix < want)
				if err != nil {
					t.Fatal(err)
				}
				runs++
			}
			if runs > bits.Len(uint(n-1)) {
				t.Fatalf("n=%d culprit=%d required %d probes", n, want, runs)
			}
		}
	}
}

func TestWavePrefixRejectsUnexpectedResults(t *testing.T) {
	t.Parallel()
	for _, s := range []WavePrefixSearch{{Green: -1, Red: 4}, {}, {Green: 4, Red: 4}} {
		if _, _, err := s.Next(); err == nil {
			t.Fatalf("accepted invalid bounds %+v", s)
		}
	}
	s := WavePrefixSearch{Red: 8}
	if _, err := s.Observe(3, true); err == nil {
		t.Fatal("accepted an unrequested prefix")
	}
	if _, err := (WavePrefixSearch{Red: 1}).Observe(1, true); err == nil {
		t.Fatal("accepted a result after finding the culprit")
	}
}
