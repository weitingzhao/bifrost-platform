package codeguard

// Assembly's imports of compute packages compute has not made public
// (assembly_contracts_test.go). Empty since 2026-11-22: compute listed the
// nine packages assembly wires (constructors, handlers, ports) under
// contracts.public_packages. The list only shrinks.
var _ = heldAssemblyReach()
