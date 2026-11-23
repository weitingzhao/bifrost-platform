package codeguard

// Assembly's imports of delivery packages delivery has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/server -> api/internal/headrelease (delivery)",
)
