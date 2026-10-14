package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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
// again under its locks (claim.go). Scores and quota (⑤, ⑥) are 2.4.

// capabilityLock is the lock on what routing reads from the Owner's rules:
// job candidates and capability facts. A claim holds it shared from its start
// to its commit; every write of job_candidate or capability_fact takes it
// exclusively first (LockCapabilities), so what a claim checks cannot change
// before the claim commits (review 154 A1).
const capabilityLock = 742106

// LockCapabilities takes the capability lock exclusively. Every transaction
// that writes job_candidate or capability_fact takes it before anything else.
func LockCapabilities(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, capabilityLock)
	return err
}

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
	id, status, executor, provider, pattern string
	version                                 int
	rule                                    ruleRef
}

// ruleRef is the signed rule a candidate or a fact came from, if any.
type ruleRef struct {
	id      *string
	version *int
}

func (r ruleRef) record() any {
	if r.id == nil {
		return nil
	}
	m := map[string]any{"id": *r.id}
	if r.version != nil {
		m["version"] = *r.version
	}
	return m
}

// routeAlgorithm names the hard checks and the input format below; a recorded
// input of another algorithm is not replayed by this one (review 155 B1).
const routeAlgorithm = 1

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
}

func (f restriction) holds(tl Tool) bool {
	return f.executor == tl.Executor && (f.provider == nil || *f.provider == tl.Provider)
}

// priorAttempt is an attempt of a task's subject; a nil provider was never
// recorded.
type priorAttempt struct {
	id, executor string
	provider     *string
}

// capabilities are the routing inputs of the Owner's rules: each job's
// candidates that are active or on probation, active first and then by id,
// and the restrictions in force, by id.
type capabilities struct {
	candidates map[string][]jobCandidate
	facts      []restriction
}

func readCapabilities(ctx context.Context, tx pgx.Tx) (capabilities, error) {
	cp := capabilities{candidates: map[string][]jobCandidate{}}
	rows, err := tx.Query(ctx, `SELECT job_id, job_candidate_id, status, executor_actor_id, model_provider, model_pattern, version,
	  rule_id, rule_version
	  FROM agentrt.job_candidate WHERE status IN ('active','probation')
	  ORDER BY job_id, status = 'probation', job_candidate_id`)
	if err != nil {
		return cp, err
	}
	var job string
	var jc jobCandidate
	if _, err = pgx.ForEachRow(rows, []any{&job, &jc.id, &jc.status, &jc.executor, &jc.provider, &jc.pattern, &jc.version,
		&jc.rule.id, &jc.rule.version}, func() error {
		cp.candidates[job] = append(cp.candidates[job], jc)
		return nil
	}); err != nil {
		return cp, err
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
	return cp, err
}

// priorAttempts reads the attempts of the given tasks, by task, each by id.
func priorAttempts(ctx context.Context, tx pgx.Tx, tasks []string) (map[string][]priorAttempt, error) {
	out := map[string][]priorAttempt{}
	if len(tasks) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT task_id, attempt_id, executor_actor_id, model_provider FROM agentrt.attempt
	  WHERE task_id = ANY($1) ORDER BY attempt_id`, tasks)
	if err != nil {
		return nil, err
	}
	var task string
	var a priorAttempt
	_, err = pgx.ForEachRow(rows, []any{&task, &a.id, &a.executor, &a.provider}, func() error {
		out[task] = append(out[task], a)
		return nil
	})
	return out, err
}

// routeInput is everything a route of one task depends on.
type routeInput struct {
	job, core, subject string
	needs              []string
	candidates         []jobCandidate
	facts              []restriction // on the task's needs and its core label
	attempts           []priorAttempt
}

// input selects what of the capabilities and the subject's attempts bears on
// task c. Core work needs author_core, or review_core for a review or a
// verification.
func (cp capabilities) input(c candidate, attempts []priorAttempt) routeInput {
	in := routeInput{job: jobOfRole[c.role], subject: c.subject, needs: c.needs, attempts: attempts}
	if c.core {
		in.core = "author_core"
		if c.role == "review" || c.role == "verify" {
			in.core = "review_core"
		}
	}
	in.candidates = cp.candidates[in.job]
	subjects := map[string]bool{in.core: in.core != ""}
	for _, n := range c.needs {
		subjects[n] = true
	}
	for _, f := range cp.facts {
		if subjects[f.subject] {
			in.facts = append(in.facts, f)
		}
	}
	return in
}

// readInput reads the route input of task c in tx.
func readInput(ctx context.Context, tx pgx.Tx, c candidate) (routeInput, error) {
	cp, err := readCapabilities(ctx, tx)
	if err != nil {
		return routeInput{}, err
	}
	return inputWith(ctx, tx, cp, c)
}

// inputWith reads the attempts of c's subject and selects c's input from cp.
func inputWith(ctx context.Context, tx pgx.Tx, cp capabilities, c candidate) (routeInput, error) {
	var attempts []priorAttempt
	if c.subject != "" {
		by, err := priorAttempts(ctx, tx, []string{c.subject})
		if err != nil {
			return routeInput{}, err
		}
		attempts = by[c.subject]
	}
	return cp.input(c, attempts), nil
}

// exclusion applies the hard checks (②–④) to one tool and returns every one
// that excludes it, in this order. Restrictions bind whether or not the
// Owner has signed them yet: a restriction is a tightening, and tightening
// needs no signature.
//   - a cannot or must_not fact on any need of the task excludes the tool;
//   - a need that some executor holds an only fact on is open to holders only;
//   - core work needs a holder of only author_core, or only review_core for a
//     review or verification; with no holder nobody may take it;
//   - a review or verification never goes to an executor or a model provider
//     that made an attempt of its subject, nor to anyone when an attempt of
//     the subject has no recorded provider (independence cannot be shown).
func (in routeInput) exclusion(tl Tool) []string {
	var reasons []string
	restricted := false
	onlySubjects, held := map[string]bool{}, map[string]bool{}
	for _, f := range in.facts {
		switch f.kind {
		case "cannot", "must_not":
			restricted = restricted || f.holds(tl)
		case "only":
			onlySubjects[f.subject] = true
			held[f.subject] = held[f.subject] || f.holds(tl)
		}
	}
	if restricted {
		reasons = append(reasons, "cannot or must_not on a need")
	}
	for s := range onlySubjects {
		if !held[s] {
			reasons = append(reasons, "a need only others may take")
			break
		}
	}
	if in.core != "" && !held[in.core] {
		reasons = append(reasons, "core work needs only "+in.core)
	}
	if in.subject != "" {
		for _, a := range in.attempts {
			if a.executor == tl.Executor || a.provider == nil || *a.provider == tl.Provider {
				reasons = append(reasons, "not independent of the subject's attempts")
				break
			}
		}
	}
	return reasons
}

// decide is the route: the candidates in order less those the hard checks
// exclude, and each excluded one with its reasons.
func (in routeInput) decide() (route []string, excluded []any) {
	route = []string{}
	excluded = []any{}
	for _, jc := range in.candidates {
		reasons := in.exclusion(Tool{Executor: jc.executor, Provider: jc.provider})
		if len(reasons) == 0 {
			route = append(route, jc.id)
			continue
		}
		excluded = append(excluded, map[string]any{"candidate": jc.id, "reason": reasons[0], "reasons": reasons})
	}
	return route, excluded
}

// record is the input as route_decided records it.
func (in routeInput) record() map[string]any {
	opt := func(p *string) any {
		if p == nil {
			return nil
		}
		return *p
	}
	cands := []any{}
	for _, jc := range in.candidates {
		cands = append(cands, map[string]any{"id": jc.id, "status": jc.status, "executor": jc.executor, "provider": jc.provider,
			"pattern": jc.pattern, "version": jc.version, "rule": jc.rule.record()})
	}
	facts := []any{}
	for _, f := range in.facts {
		facts = append(facts, map[string]any{"id": f.id, "executor": f.executor, "provider": opt(f.provider), "kind": f.kind, "subject": f.subject,
			"rule": f.rule.record()})
	}
	attempts := []any{}
	for _, a := range in.attempts {
		attempts = append(attempts, map[string]any{"id": a.id, "executor": a.executor, "provider": opt(a.provider)})
	}
	needs := []any{}
	for _, n := range in.needs {
		needs = append(needs, n)
	}
	return map[string]any{"algorithm": routeAlgorithm, "job": in.job, "core": in.core, "subject": in.subject, "needs": needs,
		"candidates": cands, "facts": facts, "attempts": attempts}
}

// fingerprint is the sha256 of the canonical record.
func (in routeInput) fingerprint() (string, error) {
	doc, err := canon.Encode(in.record())
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(doc)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// replayInput reads a recorded input back (the inverse of record).
func replayInput(rec map[string]any) (routeInput, error) {
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	num := func(m map[string]any, k string) int {
		switch n := m[k].(type) {
		case float64:
			return int(n)
		case json.Number:
			v, _ := n.Int64()
			return int(v)
		case int:
			return n
		}
		return 0
	}
	opt := func(m map[string]any, k string) *string {
		if s, ok := m[k].(string); ok {
			return &s
		}
		return nil
	}
	list := func(k string) ([]map[string]any, error) {
		l, ok := rec[k].([]any)
		if !ok {
			return nil, fmt.Errorf("store: recorded route input has no list %s", k)
		}
		out := make([]map[string]any, 0, len(l))
		for _, x := range l {
			m, ok := x.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("store: recorded route input: %s holds a %T", k, x)
			}
			out = append(out, m)
		}
		return out, nil
	}
	if a := num(rec, "algorithm"); a != routeAlgorithm {
		return routeInput{}, fmt.Errorf("store: a route input of algorithm %d; this build replays %d", a, routeAlgorithm)
	}
	rule := func(m map[string]any) ruleRef {
		r, ok := m["rule"].(map[string]any)
		if !ok {
			return ruleRef{}
		}
		ref := ruleRef{id: opt(r, "id")}
		if _, ok := r["version"]; ok {
			v := num(r, "version")
			ref.version = &v
		}
		return ref
	}
	in := routeInput{job: str(rec, "job"), core: str(rec, "core"), subject: str(rec, "subject")}
	if l, ok := rec["needs"].([]any); ok {
		for _, n := range l {
			if s, ok := n.(string); ok {
				in.needs = append(in.needs, s)
			}
		}
	}
	cands, err := list("candidates")
	if err != nil {
		return in, err
	}
	for _, m := range cands {
		in.candidates = append(in.candidates, jobCandidate{id: str(m, "id"), status: str(m, "status"), executor: str(m, "executor"),
			provider: str(m, "provider"), pattern: str(m, "pattern"), version: num(m, "version"), rule: rule(m)})
	}
	facts, err := list("facts")
	if err != nil {
		return in, err
	}
	for _, m := range facts {
		in.facts = append(in.facts, restriction{id: str(m, "id"), executor: str(m, "executor"), kind: str(m, "kind"),
			subject: str(m, "subject"), provider: opt(m, "provider"), rule: rule(m)})
	}
	attempts, err := list("attempts")
	if err != nil {
		return in, err
	}
	for _, m := range attempts {
		in.attempts = append(in.attempts, priorAttempt{id: str(m, "id"), executor: str(m, "executor"), provider: opt(m, "provider")})
	}
	return in, nil
}

// decideRoute records the route of task t (route_decided, with its input and
// the input's fingerprint) and writes it to route_json. t must be locked.
func decideRoute(ctx context.Context, tx pgx.Tx, t taskRow) error {
	c, err := candidateOf(ctx, tx, t.id)
	if err != nil {
		return err
	}
	in, err := readInput(ctx, tx, c)
	if err != nil {
		return err
	}
	return writeRoute(ctx, tx, t, in)
}

func writeRoute(ctx context.Context, tx pgx.Tx, t taskRow, in routeInput) error {
	fp, err := in.fingerprint()
	if err != nil {
		return err
	}
	route, excluded := in.decide()
	job := in.job
	if job == "" {
		job = "none"
	}
	if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "route_decided", Actor: "controller",
		MissionID: t.mission, PlanNode: t.step, TaskID: t.id,
		Body: map[string]any{"job": job, "candidates": route, "excluded": excluded, "inputs": in.record(), "fingerprint": fp}}); err != nil {
		return err
	}
	doc, err := canon.Encode(route)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE agentrt.task SET route_json = $2::jsonb WHERE task_id = $1`, t.id, string(doc))
	return err
}

// Reroute decides the route again for each ready task whose route input
// (its job's candidates, the restrictions on its needs, its subject's
// attempts) is no longer the one its last route_decided recorded: candidates
// written, demoted or changed after the task became ready, a restriction
// added or retired, a new attempt of the subject. A task whose input did not
// change gets no new event. It returns how many routes it decided.
//
// One read finds the tasks to look at, with the capabilities read once for
// the pass; each of those is then locked (skipped if busy, for the next
// pass), its last route and its input read again under the lock, and decided
// only if they still differ, so two controllers, or a move to ready in
// between, never record the same decision twice (review 154 B1, B4).
func Reroute(ctx context.Context, db DB) (int, error) {
	var stale []string
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, candidateColumns+` WHERE t.state = 'ready' ORDER BY t.created_at, t.task_id`)
		if err != nil {
			return err
		}
		tasks, err := pgx.CollectRows(rows, scanCandidate)
		if err != nil {
			return err
		}
		last := map[string]string{}
		rows, err = tx.Query(ctx, `SELECT DISTINCT ON (e.task_id) e.task_id, COALESCE(e.body_json->>'fingerprint', '')
		  FROM agentrt.event e JOIN agentrt.task t ON t.task_id = e.task_id
		  WHERE t.state = 'ready' AND e.event_type = 'route_decided' ORDER BY e.task_id, e.event_id DESC`)
		if err != nil {
			return err
		}
		var id, sum string
		if _, err = pgx.ForEachRow(rows, []any{&id, &sum}, func() error { last[id] = sum; return nil }); err != nil {
			return err
		}
		var subjects []string
		for _, c := range tasks {
			if c.subject != "" {
				subjects = append(subjects, c.subject)
			}
		}
		cp, err := readCapabilities(ctx, tx)
		if err != nil {
			return err
		}
		attempts, err := priorAttempts(ctx, tx, subjects)
		if err != nil {
			return err
		}
		for _, c := range tasks {
			fp, err := cp.input(c, attempts[c.subject]).fingerprint()
			if err != nil {
				return err
			}
			if fp != last[c.id] {
				stale = append(stale, c.id)
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	decided := 0
	for _, id := range stale {
		hook("rerouting", id)
		did, err := rerouteOne(ctx, db, id)
		if err != nil {
			return decided, err
		}
		if did {
			decided++
		}
	}
	return decided, nil
}

func rerouteOne(ctx context.Context, db DB, taskID string) (bool, error) {
	var did bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var t taskRow
		err := tx.QueryRow(ctx, `SELECT task_id, mission_id, COALESCE(plan_node_id, ''), state, effect_class, role, task_class
		  FROM agentrt.task WHERE task_id = $1 AND state = 'ready' FOR NO KEY UPDATE SKIP LOCKED`, taskID).
			Scan(&t.id, &t.mission, &t.step, &t.state, &t.effect, &t.role, &t.class)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		hook("reroute", taskID)
		var last string
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT body_json->>'fingerprint' FROM agentrt.event
		  WHERE task_id = $1 AND event_type = 'route_decided' ORDER BY event_id DESC LIMIT 1), '')`, taskID).Scan(&last); err != nil {
			return err
		}
		c, err := candidateOf(ctx, tx, taskID)
		if err != nil {
			return err
		}
		in, err := readInput(ctx, tx, c)
		if err != nil {
			return err
		}
		fp, err := in.fingerprint()
		if err != nil || fp == last {
			return err
		}
		did = true
		return writeRoute(ctx, tx, t, in)
	})
	return did, err
}
