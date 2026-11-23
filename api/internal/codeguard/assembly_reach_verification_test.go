package codeguard

// Assembly's imports of verification packages verification has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/agentrt/planwiring -> api/internal/agentrt/rules (verification)",
	"api/internal/agentrt/planwiring -> api/internal/agentrt/store (verification)",
	"api/internal/server -> api/internal/agentrt/controller (verification)",
	"api/internal/server -> api/internal/agentrt/landing (verification)",
	"api/internal/server -> api/internal/agentrt/landingci (verification)",
	"api/internal/server -> api/internal/agentrt/landinggit (verification)",
	"api/internal/server -> api/internal/agentrt/rules (verification)",
	"api/internal/server -> api/internal/agentrt/store (verification)",
	"api/internal/server -> api/internal/unitplan (verification)",
)
