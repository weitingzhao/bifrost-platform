package store

import (
	"context"
	"path"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Routing in 2.2 (design v5 §5.1.2 「分流顺序」 ①–④; review 142 B5). Each
// role has a job; the job's candidates are written by the Owner's signed
// initial rule (O19), so until it is signed no task has anyone to go to.
// When a task becomes ready its route is decided once: the job's candidates
// that are active or on probation, active first and then by id, less those
// the hard checks exclude, each exclusion with its reason (route_decided);
// the order is task.route_json. A claim walks the route and checks again,
// under its locks, against what holds then. Scores and quota (⑤, ⑥) are 2.4.

// jobOfRole is the job of each role. A role with no job (other) is not
// dispatched.
var jobOfRole = map[string]string{
	"plan":      "planner",
	"implement": "implementer",
	"verify":    "verifier",
	"review":    "reviewer",
	"brief":     "chief_of_staff",
}

type jobCandidate struct {
	id, executor, provider, pattern string
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

// jobCandidates lists the job's candidates that are active or on probation,
// active first, then by id; with ids, only those.
func jobCandidates(ctx context.Context, tx pgx.Tx, job string, ids []string) ([]jobCandidate, error) {
	if job == "" {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT job_candidate_id, executor_actor_id, model_provider, model_pattern FROM agentrt.job_candidate
	  WHERE job_id = $1 AND status IN ('active','probation') AND ($2::text[] IS NULL OR job_candidate_id = ANY($2))
	  ORDER BY status = 'probation', job_candidate_id`, job, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (jobCandidate, error) {
		var jc jobCandidate
		scanErr := r.Scan(&jc.id, &jc.executor, &jc.provider, &jc.pattern)
		return jc, scanErr
	})
}

// decideRoute records the route of a task that just became ready
// (route_decided) and writes it to route_json.
func decideRoute(ctx context.Context, tx pgx.Tx, t taskRow) error {
	c, err := candidateOf(ctx, tx, t.id)
	if err != nil {
		return err
	}
	job := jobOfRole[t.role]
	cands, err := jobCandidates(ctx, tx, job, nil)
	if err != nil {
		return err
	}
	route := []string{}
	excluded := []any{}
	for _, jc := range cands {
		var reason string
		if reason, err = exclusion(ctx, tx, Tool{Executor: jc.executor, Provider: jc.provider}, c); err != nil {
			return err
		}
		if reason == "" {
			route = append(route, jc.id)
		} else {
			excluded = append(excluded, map[string]any{"candidate": jc.id, "reason": reason})
		}
	}
	if job == "" {
		job = "none"
	}
	if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "route_decided", Actor: "controller",
		MissionID: t.mission, PlanNode: t.step, TaskID: t.id,
		Body: map[string]any{"job": job, "candidates": route, "excluded": excluded}}); err != nil {
		return err
	}
	doc, err := canon.Encode(route)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE agentrt.task SET route_json = $2::jsonb WHERE task_id = $1`, t.id, string(doc))
	return err
}

// Reroute decides the route again for each ready task whose job's
// candidates (active or on probation) are no longer the ones its last
// route_decided looked at: when the Owner's rule adds or removes candidates,
// tasks made ready before it get a route, and a task whose candidates did
// not change gets no new event. It returns how many routes it decided.
func Reroute(ctx context.Context, db DB) (int, error) {
	type ready struct{ id, role, last string }
	var tasks []ready
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.task_id, t.role, COALESCE(last.body, '') FROM agentrt.task t
		  LEFT JOIN LATERAL (SELECT e.body_json::text AS body FROM agentrt.event e
		                     WHERE e.task_id = t.task_id AND e.event_type = 'route_decided'
		                     ORDER BY e.event_id DESC LIMIT 1) last ON true
		  WHERE t.state = 'ready' ORDER BY t.created_at, t.task_id`)
		if err != nil {
			return err
		}
		tasks, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ready, error) {
			var x ready
			scanErr := r.Scan(&x.id, &x.role, &x.last)
			return x, scanErr
		})
		return err
	}); err != nil {
		return 0, err
	}
	decided := 0
	for _, x := range tasks {
		did, err := rerouteOne(ctx, db, x.id, x.role, x.last)
		if err != nil {
			return decided, err
		}
		if did {
			decided++
		}
	}
	return decided, nil
}

func rerouteOne(ctx context.Context, db DB, taskID, role, last string) (bool, error) {
	var did bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		seen, err := routedCandidates(last)
		if err != nil {
			return err
		}
		cands, err := jobCandidates(ctx, tx, jobOfRole[role], nil)
		if err != nil {
			return err
		}
		now := map[string]bool{}
		for _, jc := range cands {
			now[jc.id] = true
		}
		if sameSet(now, seen) {
			return nil
		}
		t, err := readTask(ctx, tx, taskID, true)
		if err != nil || t.state != "ready" {
			return err
		}
		did = true
		return decideRoute(ctx, tx, t)
	})
	return did, err
}

// routedCandidates is every candidate a route_decided body looked at.
func routedCandidates(body string) (map[string]bool, error) {
	out := map[string]bool{}
	if body == "" {
		return out, nil
	}
	v, err := canon.Parse([]byte(body))
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	if list, ok := m["candidates"].([]any); ok {
		for _, id := range list {
			if s, ok := id.(string); ok {
				out[s] = true
			}
		}
	}
	if list, ok := m["excluded"].([]any); ok {
		for _, x := range list {
			if e, ok := x.(map[string]any); ok {
				if s, ok := e["candidate"].(string); ok {
					out[s] = true
				}
			}
		}
	}
	return out, nil
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
