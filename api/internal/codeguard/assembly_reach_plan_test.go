package codeguard

// Assembly's imports of plan packages plan has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/agentrt/planwiring -> api/internal/agentrt/planreadings (plan)",
	"api/internal/agentrt/planwiring -> api/internal/agentrt/planstore (plan)",
	"api/internal/server -> api/internal/agentrt/planapi (plan)",
	"api/internal/server -> api/internal/agentrt/planloop (plan)",
	"api/internal/server -> api/internal/agentrt/planpace (plan)",
	"api/internal/server -> api/internal/agentrt/planstore (plan)",
)
