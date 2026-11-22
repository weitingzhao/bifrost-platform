package codeguard

// Assembly's imports of coordination packages coordination has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/agentrt/planwiring -> api/internal/agentrt/coord (coordination)",
	"api/internal/server -> api/internal/agentrt/coord (coordination)",
	"api/internal/server -> api/internal/agentrt/coordapi (coordination)",
	"api/internal/server -> api/internal/agentrt/windownotice (coordination)",
	"api/internal/server -> api/internal/evalreadings (coordination)",
	"api/internal/server -> api/internal/threadinbox (coordination)",
)
