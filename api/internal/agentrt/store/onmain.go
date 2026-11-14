package store

import ()

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
