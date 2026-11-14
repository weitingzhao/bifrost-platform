package store

import (
	"context"
	"path"

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

// fits reports whether a host's tool is this candidate: same executor, same
// model provider, and a model the pattern matches.
func (jc jobCandidate) fits(tl Tool) bool {
	if tl.Executor != jc.executor || tl.Provider != jc.provider {
		return false
	}
	ok, err := path.Match(jc.pattern, tl.Model)
	return ok && err == nil
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

// readCapabilities reads the candidates and the restrictions routing acts on.
// A candidate counts only while the job rule it came from is in force and
// reads as its text says (review 186 A2, 187 R2): a rule that does not read
// counts as none. cannot and must_not always hold; an only fact grants its
// holder only while the rule it is bound to is in force, and a fact bound to
// no rule grants nobody (the seed's are proposals until the Owner signs
// capability-facts, 187 R3); either way it still keeps everyone else out.
func readCapabilities(ctx context.Context, tx pgx.Tx) (capabilities, error) {
	cp := capabilities{candidates: map[string][]jobCandidate{}}
	inForceIDs := map[string]bool{}
	valid := func(ref ruleRef) (bool, error) {
		if ref.id == nil {
			return false, nil
		}
		if v, seen := inForceIDs[*ref.id]; seen {
			return v, nil
		}
		r, err := inForce(ctx, tx, *ref.id)
		if err != nil {
			return false, err
		}
		inForceIDs[*ref.id] = r != nil
		return r != nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT job_id, job_candidate_id, status, executor_actor_id, model_provider, model_pattern, version,
	  rule_id, rule_version
	  FROM agentrt.job_candidate WHERE status IN ('active','probation')
	  ORDER BY job_id, status = 'probation', job_candidate_id`)
	if err != nil {
		return cp, err
	}
	type row struct {
		job string
		jc  jobCandidate
	}
	var cands []row
	var job string
	var jc jobCandidate
	if _, err = pgx.ForEachRow(rows, []any{&job, &jc.id, &jc.status, &jc.executor, &jc.provider, &jc.pattern, &jc.version,
		&jc.rule.id, &jc.rule.version}, func() error {
		cands = append(cands, row{job, jc})
		return nil
	}); err != nil {
		return cp, err
	}
	for _, c := range cands {
		ok, verr := valid(c.jc.rule)
		if verr != nil {
			return cp, verr
		}
		if ok {
			cp.candidates[c.job] = append(cp.candidates[c.job], c.jc)
		}
	}
	rows, err = tx.Query(ctx, `SELECT capability_fact_id, executor_actor_id, fact_kind, subject, model_provider, rule_id, rule_version
	  FROM agentrt.capability_fact
	  WHERE retired_at IS NULL AND fact_kind IN ('cannot','must_not','only') ORDER BY capability_fact_id`)
	if err != nil {
		return cp, err
	}
	cp.facts, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (restriction, error) {
		var f restriction
		scanErr := r.Scan(&f.id, &f.executor, &f.kind, &f.subject, &f.provider, &f.rule.id, &f.rule.version)
		return f, scanErr
	})
	if err != nil {
		return cp, err
	}
	for i := range cp.facts {
		if cp.facts[i].grants, err = valid(cp.facts[i].rule); err != nil {
			return cp, err
		}
	}
	cp.thresholds, cp.scores, err = readCompetence(ctx, tx)
	return cp, err
}
