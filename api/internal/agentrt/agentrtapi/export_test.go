package agentrtapi

import "time"

// What the external tests need of the goal route's internals: the title a
// goal gets.
var GoalTitleForTest = goalTitle

// SetPruneReadTimeoutForTest sets each repository's read bound of GET
// /prunes and gives back the restore.
func SetPruneReadTimeoutForTest(d time.Duration) func() {
	old := pruneReadTimeout
	pruneReadTimeout = d
	return func() { pruneReadTimeout = old }
}

// SetPruneCollectForTest sets the bounds of all the reads of one GET
// /prunes, and of its branches, and gives back the restore.
func SetPruneCollectForTest(all, branches time.Duration) func() {
	oldAll, oldBranches := pruneCollect, pruneBranches
	pruneCollect, pruneBranches = all, branches
	return func() { pruneCollect, pruneBranches = oldAll, oldBranches }
}
