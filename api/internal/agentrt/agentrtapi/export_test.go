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
