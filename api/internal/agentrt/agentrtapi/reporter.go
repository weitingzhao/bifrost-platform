package agentrtapi

import (
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// Seats use reporter credentials too, but are existing proposers. Use the
// authenticated token name, as writer does, never a client-supplied session
// or body field. The store still checks that a seat is registered.
func limitedReporter(p actuation.Principal) bool {
	return p.Role == actuation.RoleReporter && !strings.HasPrefix(p.Name, "seat:")
}

func proposersOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limitedReporter(actuation.PrincipalFromContext(r.Context())) {
			writeError(w, http.StatusForbidden, "forbidden", "a reporter may only register external work and append its own observations")
			return
		}
		next.ServeHTTP(w, r)
	})
}
