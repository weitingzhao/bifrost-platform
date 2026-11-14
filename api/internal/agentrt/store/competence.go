package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Competence at routing (design v5 §5.1.2 「分流顺序」 ⑤; W-31 2.4 slice ②).
// A tenant's routing rule may set a threshold (rules.Competence). Then each
// candidate's sample is what the latest in-job computation (scores.go) says
// of it on tasks like this one: same job, task class and core label, the
// candidate's executor and provider, a model its pattern matches; attempts
// and first passes summed over those rows. With at least min_samples attempts
// a candidate whose score (the Wilson bound of the summed counts) is below
// min_score_percent is excluded; with fewer, one on probation takes no core
// work. The threshold and the samples are route inputs, so a new computation
// that changes a sample, or a new version of the rule, decides the route
// again (Reroute). Without a threshold the input records nothing of it and
// routes as before.

// threshold is a routing rule's competence step.
type threshold struct {
	minPercent, minSamples int
	rule                   ruleRef
}

// scoreLine is one group of a computation's or a run's samples.
type scoreLine struct {
	source, executor, provider, model, job, class string
	core                                          bool
	attempts, firstPass                           int
}

// readCompetence reads the thresholds the routing rules in force set, by
// tenant, and when any does, the samples of the latest in-job computation
// (its event's, scores.go) and of each executor's latest benchmark run
// (benchmark.go); competenceOf sums those of a candidate.
func readCompetence(ctx context.Context, tx pgx.Tx) (map[string]threshold, []scoreLine, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT rule_id FROM agentrt.rule WHERE rule_id LIKE 'routing:%' ORDER BY 1`)
	if err != nil {
		return nil, nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, nil, err
	}
	out := map[string]threshold{}
	for _, id := range ids {
		r, rerr := inForce(ctx, tx, id)
		if rerr != nil {
			return nil, nil, rerr
		}
		if r == nil || r.Routing == nil || r.Routing.Competence == nil {
			continue
		}
		ruleID, version := id, r.Version
		out[strings.TrimPrefix(id, "routing:")] = threshold{minPercent: r.Routing.Competence.MinScorePercent,
			minSamples: r.Routing.Competence.MinSamples, rule: ruleRef{id: &ruleID, version: &version}}
	}
	if len(out) == 0 {
		return out, nil, nil
	}
	var lines []scoreLine
	last, err := lastComputation(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	if last != nil {
		lines = last.samples
	}
	bench, err := latestBenchmarks(ctx, tx)
	return out, append(lines, bench...), err
}
