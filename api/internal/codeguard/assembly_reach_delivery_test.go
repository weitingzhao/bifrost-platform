package codeguard

// Assembly's imports of delivery packages delivery has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/agentrt/planwiring -> api/internal/agentrt/deliverymetrics (delivery)",
	"api/internal/agentrt/planwiring -> api/internal/agentrt/deliveryreadings (delivery)",
	"api/internal/server -> api/internal/agentrt/deliveryapi (delivery)",
	"api/internal/server -> api/internal/agentrt/deliveryview (delivery)",
	"api/internal/server -> api/internal/agentrt/deploysource (delivery)",
	"api/internal/server -> api/internal/agentrt/migraterun (delivery)",
	"api/internal/server -> api/internal/delivery (delivery)",
	"api/internal/server -> api/internal/delivery/assembly (delivery)",
	"api/internal/server -> api/internal/gitops (delivery)",
	"api/internal/server -> api/internal/headrelease (delivery)",
	"api/internal/server -> api/internal/promote (delivery)",
	"api/internal/server -> api/internal/releasepolicy (delivery)",
	"api/internal/server -> api/internal/releases (delivery)",
	"api/internal/server -> api/internal/releasetable (delivery)",
)
