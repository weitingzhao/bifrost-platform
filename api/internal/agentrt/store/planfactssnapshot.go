package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

type planFactsSnapshot struct {
	Landings     map[string]*landing.State
	Now          time.Time
	Views        map[string]viewAdoption
	Docs         map[string]*planview.Document
	Warnings     map[string]string
	Observations map[string]planObservation
	Current      map[string]bool
	External     map[string]externalNote
	Passes       map[string]planPassSeen
}

// planPassSeen is a root's latest collector pass.
type planPassSeen struct {
	Fresh map[string]time.Time
	At    time.Time
}

// One SQL statement gives a committed snapshot even inside the runtime's
// required READ COMMITTED write transaction. No global event-id watermark is
// used: each stream's sequence is authoritative (including late commits).
func readPlanFacts(ctx context.Context, tx pgx.Tx) (*planFactsSnapshot, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `WITH views AS (
 SELECT DISTINCT ON(event_stream_id) body_json->'plan_view' a FROM agentrt.event WHERE event_type='note' AND body_json ? 'plan_view' ORDER BY event_stream_id,seq DESC
 ), docs AS (
 SELECT a,encode(b.body,'base64') body FROM views LEFT JOIN agentrt.artifact_blob b ON b.artifact_id=a->>'artifact_id'
 ), view_nodes AS (
 SELECT n.value FROM views CROSS JOIN LATERAL jsonb_each_text(a->'nodes') n
 ), observations AS (
 SELECT DISTINCT ON(body_json->'plan_observation'->>'binding_sha256') body_json->'plan_observation' o FROM agentrt.event WHERE event_type='note' AND body_json ? 'plan_observation' AND actor_id='controller'
 ORDER BY body_json->'plan_observation'->>'binding_sha256',(body_json->'plan_observation'->>'source_version')::bigint DESC
 ), passes AS (
 SELECT p.* FROM views v CROSS JOIN LATERAL (SELECT body_json->'plan_observation_pass' pass FROM agentrt.event WHERE event_stream_id='plan-observation-pass:'||(v.a->>'root') AND actor_id='controller' AND body_json ? 'plan_observation_pass' ORDER BY seq DESC LIMIT 1) p
 ), external AS (
 SELECT DISTINCT ON(event_stream_id) body_json->'external_work' e FROM agentrt.event WHERE event_type='note' AND body_json ? 'external_work' ORDER BY event_stream_id,seq DESC
 ) SELECT jsonb_build_object('now',statement_timestamp(),
 'views',COALESCE((SELECT jsonb_agg(a) FROM docs),'[]'),
 'docs',COALESCE((SELECT jsonb_agg(jsonb_build_object('root',a->>'root','body',body)) FROM docs),'[]'),
 'observations',COALESCE((SELECT jsonb_agg(o) FROM observations),'[]'),
 'external',COALESCE((SELECT jsonb_agg(e) FROM external),'[]'),
 'passes',COALESCE((SELECT jsonb_agg(pass) FROM passes),'[]'),
 'landings',COALESCE((SELECT jsonb_agg(jsonb_build_object('stream',event_stream_id,'at',at,'record',body_json->'landing') ORDER BY event_stream_id,seq) FROM agentrt.event WHERE event_type='note' AND actor_id='controller' AND event_stream_id LIKE 'landing:%' AND body_json ? 'landing'),'[]'),
 'current',COALESCE((SELECT jsonb_object_agg(a.attempt_id,jsonb_build_object('task',t.task_id,'attempt',a.attempt_id,'mission',t.mission_id,'step',t.plan_node_id,'repo',t.repo,'result',a.pushed_commit,'round',t.round_no))
 FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) JOIN agentrt.attempt a USING(task_id) JOIN agentrt.plan_node n ON n.plan_node_id=t.plan_node_id
 WHERE m.tenant='dev' AND (n.plan_node_id IN (SELECT value FROM view_nodes) OR n.parent_plan_node_id IN (SELECT value FROM view_nodes))
 AND a.attempt_no=(SELECT max(attempt_no) FROM agentrt.attempt WHERE task_id=t.task_id) AND t.state NOT IN ('rework','cancelled','stopped','dropped')
 ),'{}'))`).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var rows struct {
		Landings []struct {
			Stream string
			At     time.Time
			Record json.RawMessage
		}
		Now   time.Time
		Views []viewAdoption
		Docs  []struct {
			Root string
			Body []byte
		}
		Observations []planObservation
		Current      map[string]planview.Binding
		External     []externalNote
		Passes       []planObservationPass
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	s := &planFactsSnapshot{Landings: map[string]*landing.State{}, Now: rows.Now, Views: map[string]viewAdoption{}, Docs: map[string]*planview.Document{}, Warnings: map[string]string{}, Observations: map[string]planObservation{}, Current: map[string]bool{}, External: map[string]externalNote{}, Passes: map[string]planPassSeen{}}
	for _, v := range rows.Views {
		s.Views[v.Root] = v
	}
	for _, r := range rows.Docs {
		// Parse the original bytes here: a SQL JSON cast (or extracting bindings
		// before validation) lets one corrupt artifact abort every root's read.
		d, e := planview.Parse(r.Body)
		if e == nil && (d.Root != r.Root || d.Tenant != s.Views[r.Root].Tenant || d.Version != s.Views[r.Root].Version) {
			e = fmt.Errorf("document identity does not match adoption")
		}
		if e != nil {
			detail := []rune(strings.Join(strings.Fields(e.Error()), " "))
			if len(detail) > 180 {
				detail = append(detail[:180], '…')
			}
			s.Warnings[r.Root] = fmt.Sprintf("Plan root %s: view document unavailable: %s", r.Root, string(detail))
			continue
		}
		s.Docs[r.Root] = d
		bs, _ := d.Bindings() // Parse already validated every binding.
		for _, b := range bs {
			c, ok := rows.Current[b.Attempt]
			s.Current[b.Digest()] = ok && c.Task == b.Task && c.Mission == b.Mission && c.Step == b.Step && c.Repo == b.Repo && c.Result == b.Result && c.Round == b.Round
		}
	}
	for _, o := range rows.Observations {
		s.Observations[o.Digest] = o
	}
	// A pass that read a binding again with the same answer is its latest
	// success: the answer was not rewritten (CollectPlanObservations).
	for _, p := range rows.Passes {
		seen := planPassSeen{Fresh: map[string]time.Time{}}
		var e error
		if seen.At, e = time.Parse(time.RFC3339Nano, p.At); e != nil {
			return nil, e
		}
		for d, at := range p.Fresh {
			if seen.Fresh[d], e = time.Parse(time.RFC3339Nano, at); e != nil {
				return nil, e
			}
		}
		s.applyPass(p.Root, seen)
	}
	for _, e := range rows.External {
		s.External[e.ID] = e
	}
	for _, r := range rows.Landings {
		v, e := canon.Parse(r.Record)
		if e != nil {
			return nil, e
		}
		record, e := landing.Parse(v)
		if e != nil {
			return nil, e
		}
		state := s.Landings[r.Stream]
		if state == nil {
			state = &landing.State{}
			s.Landings[r.Stream] = state
		}
		if e = state.Apply(landing.Entry{At: r.At, Record: record}); e != nil {
			return nil, e
		}
	}
	return s, nil
}

// applyPass records a root's latest pass and moves each binding's last
// success to the time the pass recorded for it, when later.
func (s *planFactsSnapshot) applyPass(root string, p planPassSeen) {
	s.Passes[root] = p
	for d, seen := range p.Fresh {
		o, ok := s.Observations[d]
		if !ok || o.Status != "Fresh" || o.Root != root {
			continue
		}
		if at, e := time.Parse(time.RFC3339Nano, o.LastSuccess); e == nil && seen.After(at) {
			o.LastSuccess = timeText(seen)
			s.Observations[d] = o
		}
	}
}

func (s *planFactsSnapshot) find(step, atom, id string, version int) (planview.Binding, bool) {
	for _, d := range s.Docs {
		bs, _ := d.Bindings()
		for _, b := range bs {
			if b.Step == step && b.Atom == atom && b.ID == id && b.Version == version {
				return b, true
			}
		}
	}
	return planview.Binding{}, false
}
func (s *planFactsSnapshot) reading(b planview.Binding) (plan.Tri, string, planObservation) {
	o := s.Observations[b.Digest()]
	if b.Task != "" && !s.Current[b.Digest()] {
		return plan.Unknown, "Unknown", o
	}
	if b.ExternalWork != "" && !externalMatches(s.External[b.ExternalWork], b) {
		return plan.Unknown, "Unknown", o
	}
	if o.Digest == "" {
		return plan.Unknown, "Unknown", o
	}
	at, e := time.Parse(time.RFC3339Nano, o.LastSuccess)
	if e == nil && s.Now.Before(at) {
		return plan.Unknown, "Unknown", o
	}
	if e == nil && s.Now.Sub(at) > planObservationTTL {
		return plan.Unknown, "Stale", o
	}
	if o.Status != "Fresh" {
		return plan.Unknown, "Unknown", o
	}
	switch o.Value {
	case "true":
		return plan.True, "Fresh", o
	case "false":
		return plan.False, "Fresh", o
	}
	return plan.Unknown, "Unknown", o
}
func (s *planFactsSnapshot) bindingValue(b planview.Binding, visiting map[string]bool) plan.Tri {
	value, _, _ := s.reading(b)
	if value != plan.True {
		return value
	}
	d := s.Docs[b.Root]
	if d == nil {
		return plan.Unknown
	}
	for _, n := range d.Items {
		if n.Node == b.Node {
			milestone := ""
			for _, a := range n.Acceptance {
				if a.ID == b.Acceptance {
					milestone = a.Milestone
				}
			}
			for _, edge := range n.Dependencies {
				if edge.Relation == "blocks" && edge.RequiredFor == milestone {
					v := s.milestone(edge.Root, edge.Node, edge.Milestone, visiting)
					if v != plan.True {
						return v
					}
				}
			}
		}
	}
	return value
}
func (s *planFactsSnapshot) milestone(root, node, milestone string, visiting map[string]bool) plan.Tri {
	key := root + "/" + node + "/" + milestone
	if visiting[key] {
		return plan.Unknown
	}
	visiting[key] = true
	defer delete(visiting, key)
	d := s.Docs[root]
	if d == nil {
		return plan.Unknown
	}
	bs, _ := d.Bindings()
	result := plan.True
	found := false
	for _, n := range d.Items {
		if n.Node != node {
			continue
		}
		for _, a := range n.Acceptance {
			if a.Milestone != milestone {
				continue
			}
			found = true
			bound := false
			for _, b := range bs {
				if b.Node != node || b.Acceptance != a.ID {
					continue
				}
				bound = true
				v := s.bindingValue(b, visiting)
				if v == plan.False {
					return plan.False
				}
				if v == plan.Unknown {
					result = plan.Unknown
				}
			}
			if !bound {
				result = plan.Unknown
			}
		}
	}
	if !found {
		return plan.Unknown
	}
	return result
}
func (f *stepFacts) BoundFact(atom, id string, version int) plan.Tri {
	if f.err != nil {
		return plan.Unknown
	}
	if f.planFacts == nil {
		f.planFacts, f.err = readPlanFacts(f.ctx, f.tx)
		if f.err != nil {
			return plan.Unknown
		}
	}
	b, ok := f.planFacts.find(f.step, atom, id, version)
	if !ok {
		return plan.Unknown
	}
	return f.planFacts.bindingValue(b, map[string]bool{})
}
