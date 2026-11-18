package agentrtapi

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
)

// WritersForTest are who a request is recorded as by writer() and
// actorOf(): the full-route test holds the old roles to the base commit's
// mapping.
func WritersForTest(r *http.Request) (w, a actor.Writer, werr, aerr error) {
	w, werr = writer(r)
	a, aerr = actorOf(r)
	return
}
