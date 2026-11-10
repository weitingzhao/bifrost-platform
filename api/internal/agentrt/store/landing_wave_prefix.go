package store

import "fmt"

// WavePrefixSearch locates the first failing commit in an ordered wave.
// Green is the largest known green prefix (zero means the base); Red is
// the smallest known red prefix, initially the entire wave. Prefix probes
// are diagnostic landing CIs and must never authorize a push to main.
// The search assumes a green-to-red boundary: later commits do not repair
// the failure being diagnosed.
type WavePrefixSearch struct {
	Green int
	Red   int
}

// Next returns the next prefix length, or the one-based culprit index
// when the bounds are adjacent. The caller must test members[:prefix],
// preserving their order and ancestry, rather than an independent half.
func (s WavePrefixSearch) Next() (prefix, culprit int, err error) {
	if s.Green < 0 || s.Red <= s.Green {
		return 0, 0, fmt.Errorf("invalid wave prefix bounds: green=%d red=%d", s.Green, s.Red)
	}
	if s.Red-s.Green == 1 {
		return 0, s.Red, nil
	}
	return s.Green + (s.Red-s.Green)/2, 0, nil
}

// Observe narrows the bounds after the requested diagnostic CI finishes.
// Requiring the pending midpoint prevents stale or unrelated CI results
// from changing the search. A green diagnostic still requires another CI
// on the complete repaired wave before landing.
func (s WavePrefixSearch) Observe(prefix int, green bool) (WavePrefixSearch, error) {
	next, culprit, err := s.Next()
	if err != nil {
		return s, err
	}
	if culprit != 0 || prefix != next {
		return s, fmt.Errorf("unexpected wave prefix result: got %d, pending %d", prefix, next)
	}
	if green {
		s.Green = prefix
	} else {
		s.Red = prefix
	}
	return s, nil
}
