package store

import "testing"

// What main holds of a task, and the route it took (RecordLandings): the
// three ways main names it and the landing stream's word over the rest.
func TestOnMainMatch(t *testing.T) {
	t.Parallel()
	const (
		a1    = "1111111111111111111111111111111111111111"
		a2    = "2222222222222222222222222222222222222222"
		other = "3333333333333333333333333333333333333333"
		main  = "4444444444444444444444444444444444444444"
	)
	pushed := []string{a2, a1} // newest first
	for name, c := range map[string]struct {
		entries  map[string]string
		accepted string
		landed   bool
		on       bool
		want     onMainMatch
	}{
		"nothing on main":           {entries: nil},
		"empty entry":               {entries: map[string]string{a1: ""}},
		"its result as it is":       {entries: map[string]string{a1: a1}, on: true, want: onMainMatch{a1, a1, a1, MatchedResult, LandedViaOwnerMerge}},
		"its result cherry-picked":  {entries: map[string]string{a1: main}, on: true, want: onMainMatch{a1, main, a1, MatchedCherryPick, LandedViaHarvest}},
		"a trailer naming the task": {entries: map[string]string{other: main}, on: true, want: onMainMatch{other, main, "", MatchedTrailer, LandedViaOwnerMerge}},
		"the accepted one first": {entries: map[string]string{a1: main, a2: a2, other: other}, accepted: a1, on: true,
			want: onMainMatch{a1, main, a1, MatchedCherryPick, LandedViaHarvest}},
		"then the newest pushed": {entries: map[string]string{a1: main, a2: a2, other: other}, on: true,
			want: onMainMatch{a2, a2, a2, MatchedResult, LandedViaOwnerMerge}},
		"pushed before a trailer": {entries: map[string]string{a1: main, other: other}, on: true,
			want: onMainMatch{a1, main, a1, MatchedCherryPick, LandedViaHarvest}},
		"the landing stream landed it": {entries: map[string]string{a1: main}, landed: true, on: true,
			want: onMainMatch{a1, main, a1, MatchedCherryPick, LandedViaLanding}},
		"landed, by a trailer": {entries: map[string]string{other: main}, landed: true, on: true,
			want: onMainMatch{other, main, "", MatchedTrailer, LandedViaLanding}},
	} {
		got, on := matchOnMain(c.entries, c.accepted, pushed, c.landed)
		if on != c.on || got != c.want {
			t.Errorf("%s: %+v %v; want %+v %v", name, got, on, c.want, c.on)
		}
	}
}
