package server

import (
	_ "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/legacyvocab" // the closed vocabulary's types no domain declares
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planwiring"
)

// The runtime's start (decoupling item 5, S4, 2026-11-18): the plan's ports
// onto the work records are bound here by name, where the API binary
// assembles its parts. planwiring binds the same when it is linked, and all
// its other ports; binding twice is harmless (an atomic store).
func init() { planstore.BindPlanExternalWork(planwiring.ExternalWorkPorts()) }
