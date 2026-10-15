package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The rows a rule writes besides its own (design v5 §5.1.2, proposal §2): a
// job's brief and its candidates (job:<id>), and the Owner's capability facts
// (capability-facts). A signed rule states them whole: a candidate it leaves
// out is retired, an Owner fact it leaves out is retired, and the facts it
// lists that are already recorded (the seed's among them) are bound to it. A
// tightening only lowers: a candidate's status never goes up from what the
// table has, and facts are only added.

// candidateStatus ranks a candidate's statuses, lowest first.
var candidateStatus = []string{"retired", "demoted", "qualifying", "probation", "active"}

type candidateRow struct {
	id       string
	c        rules.Candidate
	from, to string
}

type factRow struct {
	id string
	f  rules.Fact
}

type ruleRows struct {
	jobs                []string
	candidates          []candidateRow
	newFacts            []factRow
	bindFacts, oldFacts []string
}

func (p ruleRows) candidateIDs() []string {
	ids := make([]string, 0, len(p.candidates))
	for _, c := range p.candidates {
		ids = append(ids, c.id)
	}
	return ids
}

func (p ruleRows) factIDs() []string {
	ids := slices.Concat(p.bindFacts, p.oldFacts)
	for _, f := range p.newFacts {
		ids = append(ids, f.id)
	}
	return ids
}

// candidateID names a new candidate row.
func candidateID(job string, c rules.Candidate) string {
	return job + "/" + c.Executor + "/" + c.Provider + "/" + c.ModelPattern
}

// planRows reads, under lock, the rows the rule touches and decides what to
// write, so the event can name every one of them first.
// A tightening, signed or not, never raises a candidate above the table's
// status (review 186 A5): only a rule marked loosen (signed) may.
func planRows(ctx context.Context, tx pgx.Tx, r rules.Rule, signed, tightening bool) (ruleRows, error) {
	var p ruleRows
	switch r.Kind {
	case rules.KindJob:
		return p, p.planJob(ctx, tx, r, signed, tightening)
	case rules.KindFacts:
		return p, p.planFacts(ctx, tx, r, signed)
	}
	return p, nil
}

func (p *ruleRows) planJob(ctx context.Context, tx pgx.Tx, r rules.Rule, signed, tightening bool) error {
	job := r.Job.JobID
	rows, err := tx.Query(ctx, `SELECT job_candidate_id, executor_actor_id, model_provider, model_pattern, status
	  FROM agentrt.job_candidate WHERE job_id = $1 ORDER BY job_candidate_id FOR UPDATE`, job)
	if err != nil {
		return err
	}
	type have struct {
		id, status string
		c          rules.Candidate
	}
	existing := map[string]have{}
	var order []string
	for rows.Next() {
		var h have
		if err = rows.Scan(&h.id, &h.c.Executor, &h.c.Provider, &h.c.ModelPattern, &h.status); err != nil {
			rows.Close()
			return err
		}
		k := candidateID(job, h.c)
		existing[k] = h
		order = append(order, k)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	listed := map[string]bool{}
	add := func(c rules.Candidate, to string) {
		k := candidateID(job, c)
		row := candidateRow{id: k, c: c, to: to}
		if h, ok := existing[k]; ok {
			row.id, row.from = h.id, h.status
			if tightening && slices.Index(candidateStatus, to) > slices.Index(candidateStatus, h.status) {
				row.to = h.status // a tightening never raises what the table has
			}
		}
		if signed || row.to != row.from {
			p.candidates = append(p.candidates, row)
		}
	}
	for _, c := range r.Job.Candidates {
		listed[candidateID(job, c)] = true
		add(c, c.Status)
	}
	for _, k := range order {
		if h := existing[k]; !listed[k] && h.status != "retired" {
			add(h.c, "retired")
		}
	}
	if signed {
		p.jobs = []string{job}
	}
	return nil
}

func factKey(f rules.Fact) string {
	scope, _ := json.Marshal(f.Scope)
	return f.Executor + "|" + f.Provider + "|" + f.Kind + "|" + f.Subject + "|" + string(scope)
}

func (p *ruleRows) planFacts(ctx context.Context, tx pgx.Tx, r rules.Rule, signed bool) error {
	rows, err := tx.Query(ctx, `SELECT capability_fact_id, executor_actor_id, COALESCE(model_provider, ''), fact_kind, subject, scope_json
	  FROM agentrt.capability_fact WHERE source_kind = 'owner' AND retired_at IS NULL ORDER BY capability_fact_id FOR UPDATE`)
	if err != nil {
		return err
	}
	live := map[string]string{}
	var order []string
	for rows.Next() {
		var id string
		var f rules.Fact
		var scope []byte
		if err = rows.Scan(&id, &f.Executor, &f.Provider, &f.Kind, &f.Subject, &scope); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(scope, &f.Scope); err != nil {
			rows.Close()
			return err
		}
		if len(f.Scope) == 0 {
			f.Scope = nil
		}
		if _, dup := live[factKey(f)]; !dup {
			live[factKey(f)] = id
			order = append(order, factKey(f))
		} else {
			p.oldFacts = append(p.oldFacts, id) // a duplicate of a fact already kept
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	kept := map[string]bool{}
	for _, f := range r.Facts {
		k := factKey(f)
		if id, ok := live[k]; ok {
			kept[k] = true
			if signed {
				p.bindFacts = append(p.bindFacts, id)
			}
			continue
		}
		p.newFacts = append(p.newFacts, factRow{id: "cf_" + newULID(time.Now()), f: f})
	}
	if !signed {
		p.oldFacts = nil
		return nil
	}
	for _, k := range order {
		if !kept[k] {
			p.oldFacts = append(p.oldFacts, live[k])
		}
	}
	return nil
}

// write writes the planned rows, each under an event that names it.
func (p ruleRows) write(ctx context.Context, tx pgx.Tx, w Writer, r rules.Rule, signed bool) error {
	if len(p.jobs) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ($1, $2, $3, $4)
		  ON CONFLICT (job_id) DO UPDATE SET tenant = EXCLUDED.tenant, rule_id = EXCLUDED.rule_id, rule_version = EXCLUDED.rule_version`,
			r.Job.JobID, r.Job.Tenant, r.ID, r.Version); err != nil {
			return err
		}
	}
	for _, c := range p.candidates {
		if !signed {
			if _, err := Append(ctx, tx, Event{Stream: "system", Type: "candidate_status_changed", Actor: w.Actor,
				Body: map[string]any{"job_candidate_id": c.id, "from": c.from, "to": c.to, "rule_id": r.ID, "rule_version": r.Version}}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider,
		    model_pattern, status, rule_id, rule_version) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		  ON CONFLICT (job_candidate_id) DO UPDATE SET status = EXCLUDED.status, rule_id = EXCLUDED.rule_id,
		    rule_version = EXCLUDED.rule_version, version = agentrt.job_candidate.version + 1`,
			c.id, r.Job.JobID, c.c.Executor, c.c.Provider, c.c.ModelPattern, c.to, r.ID, r.Version); err != nil {
			return err
		}
	}
	for _, f := range p.newFacts {
		scope := f.f.Scope
		if scope == nil {
			scope = map[string]any{}
		}
		doc, err := json.Marshal(scope)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.capability_fact (capability_fact_id, executor_actor_id, model_provider, fact_kind,
		    subject, scope_json, source_kind, source, rule_id, rule_version) VALUES ($1, $2, $3, $4, $5, $6::jsonb, 'owner', $7, $8, $9)`,
			f.id, f.f.Executor, nullable(f.f.Provider), f.f.Kind, f.f.Subject, string(doc), fmt.Sprintf("rule %s v%d", r.ID, r.Version),
			r.ID, r.Version); err != nil {
			return err
		}
	}
	if len(p.bindFacts) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE agentrt.capability_fact SET rule_id = $2, rule_version = $3 WHERE capability_fact_id = ANY($1)`,
			p.bindFacts, r.ID, r.Version); err != nil {
			return err
		}
	}
	if len(p.oldFacts) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE agentrt.capability_fact SET retired_at = clock_timestamp() WHERE capability_fact_id = ANY($1)`,
			p.oldFacts); err != nil {
			return err
		}
	}
	return nil
}
