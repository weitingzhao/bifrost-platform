package planapi

import (
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// HandleGetLaws is GET /laws: the register of laws by number, each as it
// stands (its version, text, status and the card that last changed it), and
// for a law an approved card changed, its plan item and how many of its
// standards are reached, missed and unmeasured (closed loop, standard 2;
// planstore.LawsView). Read-only. framework is the framework of laws itself
// (levels, fields, subjects, rules, states with their counts), so a page
// shows the frame even when no law is in force.
func (rt Routes) HandleGetLaws(w http.ResponseWriter, r *http.Request) {
	laws, err := planstore.LawsView(r.Context(), rt.Host.DB())
	if err != nil {
		rt.Host.Logf("laws: %v", err)
		rt.Host.Error(w, 503, "unavailable", "the register of laws is unavailable")
		return
	}
	if laws == nil {
		laws = []planstore.LawView{}
	}
	plain := make([]planstore.Law, len(laws))
	for i, l := range laws {
		plain[i] = l.Law
	}
	rt.Host.JSON(w, 200, map[string]any{"framework": planstore.TheFramework(plain, time.Now()), "laws": laws})
}
