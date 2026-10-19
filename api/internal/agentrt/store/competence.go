package store

import (
	"context"
	"path"
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

// scoreLine is one row of the latest in-job computation.
type scoreLine struct {
	executor, provider, model, job, class string
	core                                  bool
	attempts, firstPass                   int
}

// sample is a candidate's summed counts on one kind of task.
type sample struct{ attempts, firstPass int }

// competence is the route input of step ⑤: the threshold and each
// candidate's sample.
type competence struct {
	threshold
	samples map[string]sample
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

// competenceOf is task c's step ⑤ input, or nil when its tenant's routing
// rule sets no threshold.
func (cp capabilities) competenceOf(c candidate, job string, cands []jobCandidate) *competence {
	th, ok := cp.thresholds[c.tenant]
	if !ok {
		return nil
	}
	cm := &competence{threshold: th, samples: map[string]sample{}}
	for _, jc := range cands {
		var s sample
		for _, l := range cp.scores {
			if l.job != job || l.class != c.class || l.core != c.core || l.executor != jc.executor || l.provider != jc.provider {
				continue
			}
			if ok, err := path.Match(jc.pattern, l.model); ok && err == nil {
				s.attempts += l.attempts
				s.firstPass += l.firstPass
			}
		}
		cm.samples[jc.id] = s
	}
	return cm
}

// unfit applies step ⑤ to one candidate and returns why it may not take the
// task, if it may not.
func (in routeInput) unfit(jc jobCandidate) []string {
	cm := in.competence
	if cm == nil {
		return nil
	}
	s := cm.samples[jc.id]
	if s.attempts >= cm.minSamples {
		if Wilson(s.firstPass, s.attempts)*100 < float64(cm.minPercent) {
			return []string{"below the competence threshold"}
		}
		return nil
	}
	if jc.status == "probation" && in.core != "" {
		return []string{"too few samples for core work"}
	}
	return nil
}

// record is the competence input as route_decided records it.
func (cm *competence) record(cands []jobCandidate) map[string]any {
	samples := []any{}
	for _, jc := range cands {
		s := cm.samples[jc.id]
		samples = append(samples, map[string]any{"candidate": jc.id, "attempts": s.attempts, "first_pass": s.firstPass})
	}
	return map[string]any{"min_score_percent": cm.minPercent, "min_samples": cm.minSamples, "rule": cm.rule.record(),
		"samples": samples}
}
