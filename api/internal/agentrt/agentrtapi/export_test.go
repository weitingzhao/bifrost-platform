package agentrtapi

import (
	"net/http"

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
