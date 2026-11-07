package store

import (
	"slices"
	"sort"
)

// How a task's change was found on main (matched_by) and the route it took
// there (landed_via): the landed note's fields (RecordLandings).
const (
	MatchedResult     = "result"      // a pushed commit of the task is the main commit itself
	MatchedCherryPick = "cherry_pick" // a pushed commit of the task stands on main as another commit
	MatchedTrailer    = "trailer"     // main names the task by a commit it never pushed (its trailer)

	LandedViaLanding    = "landing"     // the repository's landing stream landed an operation of the task
	LandedViaHarvest    = "harvest"     // cherry-picked with -x (merge_accepted.py, the coordinator's batch merges)
	LandedViaOwnerMerge = "owner_merge" // anything else: the Owner's or the coordinator's own merge
)

// onMainMatch is what main holds of one task: Key, the commit OnMain names
// it by; Main, the commit it stands on main as; Result, Key when it is one of
// the task's pushed commits, else "".
type onMainMatch struct {
	Key, Main, Result string
	MatchedBy, Via    string
}

// matchOnMain says whether entries (OnMain[task]) put the task on main, and
// how. It prefers the accepted attempt's commit, then the task's pushed
// commits (pushed newest first), then any other entry (in order of its key).
// landed is whether the landing stream has an operation of the task that
// landed: the route is then landing, whatever matched.
func matchOnMain(entries map[string]string, accepted string, pushed []string, landed bool) (onMainMatch, bool) {
	var key string
	for _, c := range append([]string{accepted}, pushed...) {
		if c != "" && entries[c] != "" {
			key = c
			break
		}
	}
	if key == "" {
		others := make([]string, 0, len(entries))
		for k, v := range entries {
			if v != "" {
				others = append(others, k)
			}
		}
		if len(others) == 0 {
			return onMainMatch{}, false
		}
		sort.Strings(others)
		key = others[0]
	}
	m := onMainMatch{Key: key, Main: entries[key], MatchedBy: MatchedTrailer}
	if key == accepted || slices.Contains(pushed, key) {
		m.Result, m.MatchedBy = key, MatchedCherryPick
		if key == m.Main {
			m.MatchedBy = MatchedResult
		}
	}
	switch {
	case landed:
		m.Via = LandedViaLanding
	case m.MatchedBy == MatchedCherryPick:
		m.Via = LandedViaHarvest
	default:
		m.Via = LandedViaOwnerMerge
	}
	return m, true
}
