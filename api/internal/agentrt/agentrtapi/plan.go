package agentrtapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The plan, read (v5 §5.2: GET /plan?root=R2, GET /plan/{plan_node_id}: the
// tree and each step's state with the event it came from).

// stateEvent is the event a node's state came from, as JSON: the last state
// change, or, for a node whose state never changed, its creation.
const stateEvent = `(SELECT jsonb_build_object('event_id', e.event_id, 'event_type', e.event_type, 'actor_id', e.actor_id,
	  'at', e.at, 'body', e.body_json) FROM agentrt.event e
	  WHERE e.event_id = n.state_event_id
	     OR (n.state_event_id IS NULL AND e.plan_node_id = n.plan_node_id AND e.event_type = 'plan_node_created')
	  ORDER BY e.event_id DESC LIMIT 1)`

// taskCounts is a node's tasks counted by state, as JSON.
const taskCounts = `COALESCE((SELECT jsonb_object_agg(state, k) FROM (SELECT state, count(*) AS k FROM agentrt.task
	  WHERE plan_node_id = n.plan_node_id GROUP BY state) s), '{}'::jsonb)`

// getPlan is GET /plan: without root, the roots (rounds and the architecture
// root); with root, that node and everything under it, depth first in
// ordinal order, each with its depth, state event and task counts. A root
// there is not is 404.
func (a *API) getPlan(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		a.list(w, r, `SELECT jsonb_build_object('roots', COALESCE(jsonb_agg(to_jsonb(n) ORDER BY n.node_kind, n.ordinal, n.plan_node_id),
		  '[]'::jsonb)) FROM agentrt.plan_node n WHERE n.parent_plan_node_id IS NULL`)
		return
	}
	a.getRow(w, r, `WITH RECURSIVE tree AS (
	    SELECT p.plan_node_id, ARRAY[p.ordinal] AS path, 0 AS depth FROM agentrt.plan_node p WHERE p.plan_node_id = $1
	    UNION ALL
	    SELECT c.plan_node_id, t.path || c.ordinal, t.depth + 1
	      FROM agentrt.plan_node c JOIN tree t ON c.parent_plan_node_id = t.plan_node_id)
	  SELECT jsonb_build_object('root', $1::text, 'nodes', jsonb_agg(to_jsonb(n) || jsonb_build_object('depth', t.depth,
	      'state_event', `+stateEvent+`, 'task_counts', `+taskCounts+`) ORDER BY t.path))
	  FROM tree t JOIN agentrt.plan_node n USING (plan_node_id)
	  HAVING count(*) > 0`, root)
}

// getPlanNode is GET /plan/{id}: the node with its state event, its
// children, its tasks, what it is bound to and its last 50 events.
func (a *API) getPlanNode(w http.ResponseWriter, r *http.Request) {
	a.getRow(w, r, `SELECT to_jsonb(n) || jsonb_build_object(
	    'state_event', `+stateEvent+`,
	    'task_counts', `+taskCounts+`,
	    'children', COALESCE((SELECT jsonb_agg(jsonb_build_object('plan_node_id', c.plan_node_id, 'node_kind', c.node_kind,
	        'label', c.label, 'title', c.title, 'state', c.state) ORDER BY c.ordinal)
	      FROM agentrt.plan_node c WHERE c.parent_plan_node_id = n.plan_node_id), '[]'::jsonb),
	    'tasks', COALESCE((SELECT jsonb_agg(jsonb_build_object('task_id', t.task_id, 'title', t.title, 'role', t.role,
	        'state', t.state, 'mission_id', t.mission_id) ORDER BY t.created_at, t.task_id)
	      FROM agentrt.task t WHERE t.plan_node_id = n.plan_node_id), '[]'::jsonb),
	    'bindings', COALESCE((SELECT jsonb_agg(jsonb_build_object('kind', b.bound_kind, 'ref', b.bound_ref) ORDER BY b.plan_node_binding_id)
	      FROM agentrt.plan_node_binding b WHERE b.plan_node_id = n.plan_node_id), '[]'::jsonb),
	    'events', COALESCE((SELECT jsonb_agg(jsonb_build_object('event_id', e.event_id, 'event_type', e.event_type,
	        'actor_id', e.actor_id, 'at', e.at, 'body', e.body_json) ORDER BY e.event_id)
	      FROM (SELECT * FROM agentrt.event WHERE plan_node_id = n.plan_node_id ORDER BY event_id DESC LIMIT 50) e), '[]'::jsonb))
	  FROM agentrt.plan_node n WHERE n.plan_node_id = $1`, chi.URLParam(r, "id"))
}

type planProposalBody struct {
	IdempotencyKey string             `json:"idempotency_key"`
	Root           string             `json:"root"`
	Reason         string             `json:"reason"`
	Changes        []store.PlanChange `json:"changes"`
}

// proposePlanChange is POST /plan/proposals: plan_change_proposed; within
// the standing authorization decided and applied at once, otherwise tried,
// rolled back, and put to the Owner as a decision card.
func (a *API) proposePlanChange(w http.ResponseWriter, r *http.Request) {
	var b planProposalBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.ProposePlanChange(r.Context(), a.DB, who, b.IdempotencyKey, b.Root, b.Reason, b.Changes, a.Ask)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// progress is GET /progress: the projection of
// SPEC-agentrt-progress-2026-10-14.md, computed on every read. A read that
// fails is an error, never an old copy.
func (a *API) progress(w http.ResponseWriter, r *http.Request) {
	out, err := store.Progress(r.Context(), a.DB)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
