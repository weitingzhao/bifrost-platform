package server

import (
	_ "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/legacyvocab" // the closed vocabulary's types no domain declares
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planwiring"
)

// The runtime's start (decoupling item 5, S4, 2026-11-18): the plan code's
// ports are bound here by explicit calls, where the API binary assembles its
// parts: the work-record ports by name, the rest by Bind (binding twice is
// harmless: an atomic store). Linking planwiring binds nothing; a test that
// runs the plan code calls planwiring.Bind from its TestMain.
func init() {
	planstore.BindPlanExternalWork(planwiring.ExternalWorkPorts())
	planwiring.Bind()
}
