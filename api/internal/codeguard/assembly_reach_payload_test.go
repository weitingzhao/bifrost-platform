package codeguard

// Assembly's imports of payload packages payload has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/server -> api/internal/dataclone (payload)",
	"api/internal/server -> api/internal/ibgateway (payload)",
	"api/internal/server -> api/internal/marketdata (payload)",
	"api/internal/server -> api/internal/telemetry/pluginmetrics (payload)",
	"api/internal/server -> api/internal/tenant/appreadings (payload)",
	"api/internal/server -> api/internal/tenant/assembly (payload)",
	"api/internal/server -> api/internal/tenant/husbandry/checklistview (payload)",
	"api/internal/server -> api/internal/topology (payload)",
)
