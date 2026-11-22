package codeguard

// Assembly's imports of lineage packages lineage has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/agentrt/planwiring -> api/internal/agentrt/externalwork (lineage)",
	"api/internal/agentrt/planwiring -> api/internal/agentrt/workregistration (lineage)",
	"api/internal/server -> api/internal/agentrt/externalwork (lineage)",
	"api/internal/server -> api/internal/agentrt/lineageapi (lineage)",
	"api/internal/server -> api/internal/agentrt/recorderimport (lineage)",
	"api/internal/server -> api/internal/agentrt/workregistration (lineage)",
	"api/internal/server -> api/internal/agentthreads (lineage)",
	"api/internal/server -> api/internal/lineage (lineage)",
	"api/internal/server -> api/internal/lineage/assembly (lineage)",
	"api/internal/server -> api/internal/threadtitles (lineage)",
)
