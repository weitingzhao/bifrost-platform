// Package ports is what the verification domain hands other domains' ports:
// each function here is written for one consumer and answers in plain data
// (structs of this package with only the fields that consumer reads, builtin
// and foundation types). The rule store, the rule registry, the landing
// rules and the unit plan stay inside; the assembly converts these answers
// into the consumer's own types. Added 2026-11-23 (decoupling endpoint E5:
// the assembly reaches verification only through its public packages).
package ports
