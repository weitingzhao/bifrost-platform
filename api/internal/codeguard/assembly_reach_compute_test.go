package codeguard

// Assembly's imports of compute packages compute has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/server -> api/internal/computenodes (compute)",
	"api/internal/server -> api/internal/computereadings (compute)",
	"api/internal/server -> api/internal/devsession (compute)",
	"api/internal/server -> api/internal/elasticreadings (compute)",
	"api/internal/server -> api/internal/mcp (compute)",
	"api/internal/server -> api/internal/operatorplane (compute)",
	"api/internal/server -> api/internal/patrol (compute)",
	"api/internal/server -> api/internal/placement (compute)",
	"api/internal/server -> api/internal/selfhealth (compute)",
)
