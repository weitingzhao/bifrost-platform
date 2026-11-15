package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Routing in 2.2 (design v5 §5.1.2 「分流顺序」 ①–④; review 142 B5). Each
// role has a job; the job's candidates are written by the Owner's signed
// initial rule (O19), so until it is signed no task has anyone to go to.
// When a task becomes ready its route is decided: the job's candidates that
// are active or on probation, active first and then by id, less those the
// hard checks exclude, each exclusion with its reasons; the order is
// task.route_json. A route is a function of its inputs (routeInput: the
// candidates, the restrictions on the task's needs, the attempts of its
// subject), and route_decided records them with their fingerprint, so a
// decision can be replayed and is decided again exactly when they change
// (Reroute; review 154 A2, B2, B3). A claim walks the route and checks it
// again under its locks (claim.go). Competence (⑤) is a route input too
// (competence.go); quota and rotation (⑥, ⑦) are read at the claim (pick.go).

// capabilityLock is the lock on what routing reads from the Owner's rules:
// job candidates and capability facts. A claim holds it shared from its start
// to its commit; every write of job_candidate or capability_fact takes it
// exclusively first (LockCapabilities), so what a claim checks cannot change
// before the claim commits (review 154 A1).
const capabilityLock = 742106

// LockCapabilities takes the capability lock exclusively. Every transaction
// that writes job_candidate or capability_fact takes it before any row lock
// or the lock of a stream it appends to; a rule write first ensures its
// writer (the writer's lock, and a new thread's registration lock and the
// system stream), and while holding this lock waits for none of those
// (reviews 187 R1, 188 R1).
func LockCapabilities(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, capabilityLock)
	return err
}

type jobCandidate struct {
	id, status, executor, provider, pattern string
	version                                 int
	rule                                    ruleRef
}

// ruleRef is the signed rule a candidate or a fact came from, if any.
type ruleRef struct {
	id      *string
	version *int
}

// restriction is a capability fact in force that can exclude a tool: cannot,
// must_not or only. A nil provider holds for every provider.
type restriction struct {
	id, executor, kind, subject string
	provider                    *string
	rule                        ruleRef
	// grants: an only fact makes its holder eligible only while the rule it
	// is bound to is in force and reads; otherwise (expired, unreadable, or
	// bound to no rule) it still keeps everyone else out and nobody holds it
	// (review 186 A2, 187 R2, R3).
	grants bool
}

// capabilities are the routing inputs of the Owner's rules: each job's
// candidates that are active or on probation, active first and then by id,
// the restrictions in force, by id, and the competence thresholds by tenant
// with the in-job scores they are read against.
type capabilities struct {
	candidates map[string][]jobCandidate
	facts      []restriction
	thresholds map[string]threshold
	scores     []scoreLine
}
