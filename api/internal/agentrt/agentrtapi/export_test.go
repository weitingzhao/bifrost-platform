package agentrtapi

import (
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// WritersForTest are who a request is recorded as by writer(), actorOf()
// and intakeWriter(): the full-route test holds the old roles to the base
// commit's mapping (DESIGN-crew-intake §5.2 ③).
func WritersForTest(r *http.Request) (w, a, i store.Writer, werr, aerr, ierr error) {
	w, werr = writer(r)
	a, aerr = actorOf(r)
	i, ierr = intakeWriter(r)
	return
}

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
