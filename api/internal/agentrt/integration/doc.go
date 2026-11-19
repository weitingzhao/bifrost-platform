// Package integration is where the agent runtime's cross-domain tests sit: a
// test here goes through several domains at once (plan, lineage, delivery,
// coordination, verification, the Owner's cards), so it belongs to none of
// them. It holds test files only, and no production code: nothing imports it.
//
// They left agentrt/store on 2026-11-18, so that the store's tests are about
// the rules and do not run again whenever one of those domains changes.
package integration
