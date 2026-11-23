package codeguard

// Assembly's imports of verification packages verification has not made public
// (assembly_contracts_test.go), 2026-11-22. Empty since 2026-11-23: the
// wiring that was purely verification's (the controller, the landing CI's
// client, the unit scope shadow, the run view's cache) moved into
// verification/assembly, and what assembly hands other domains' ports of
// verification's facts (the rule store, the landing rules, the unit plan)
// comes from verification/ports in plain data. The list only shrinks.
var _ = heldAssemblyReach()
