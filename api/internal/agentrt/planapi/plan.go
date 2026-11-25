package planapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
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

// taskCounts was a node's tasks counted by state. No task is proposed any
// more and the tasks' table is not read (store cut 4, 2026-11-14): the key
// stays, empty.
const taskCounts = `'{}'::jsonb`

// signoffs is a step's sign-offs (owner_signoff events: ref, by, at,
// event_id, oldest first) under "signoffs", merged into its node; a node with
// none gets no key.
const signoffs = `COALESCE((SELECT jsonb_build_object('signoffs', jsonb_agg(jsonb_build_object('ref', e.body_json->>'ref',
	  'by', e.actor_id, 'at', e.at, 'event_id', e.event_id) ORDER BY e.event_id)) FROM agentrt.event e
	  WHERE e.plan_node_id = n.plan_node_id AND e.event_type = 'owner_signoff' HAVING count(*) > 0), '{}'::jsonb)`

// HandleGetPlan is GET /plan: without root, the roots (rounds and the architecture
// root); with root, that node and everything under it, depth first in
// ordinal order, each with its depth, state event, task counts and sign-offs. A root
// there is not is 404.
func (rt Routes) HandleGetPlan(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		rt.Host.Document(w, r, `SELECT jsonb_build_object('roots', COALESCE(jsonb_agg(to_jsonb(n) ORDER BY n.node_kind, n.ordinal, n.plan_node_id),
		  '[]'::jsonb)) FROM agentrt.plan_node n WHERE n.parent_plan_node_id IS NULL`)
		return
	}
	rt.Host.Document(w, r, `WITH RECURSIVE tree AS (
	    SELECT p.plan_node_id, ARRAY[p.ordinal] AS path, 0 AS depth FROM agentrt.plan_node p WHERE p.plan_node_id = $1
	    UNION ALL
	    SELECT c.plan_node_id, t.path || c.ordinal, t.depth + 1
	      FROM agentrt.plan_node c JOIN tree t ON c.parent_plan_node_id = t.plan_node_id)
	  SELECT jsonb_build_object('root', $1::text, 'nodes', jsonb_agg(to_jsonb(n) || jsonb_build_object('depth', t.depth,
	      'state_event', `+stateEvent+`, 'task_counts', `+taskCounts+`) || `+signoffs+` ORDER BY t.path))
	  FROM tree t JOIN agentrt.plan_node n USING (plan_node_id)
	  HAVING count(*) > 0`, root)
}

// HandleGetPlanNode is GET /plan/{id}: the node with its state event, its
// children, its tasks, what it is bound to, its sign-offs and its last 50 events.
func (rt Routes) HandleGetPlanNode(w http.ResponseWriter, r *http.Request) {
	rt.Host.Document(w, r, `SELECT to_jsonb(n) || jsonb_build_object(
	    'state_event', `+stateEvent+`,
	    'task_counts', `+taskCounts+`,
	    'children', COALESCE((SELECT jsonb_agg(jsonb_build_object('plan_node_id', c.plan_node_id, 'node_kind', c.node_kind,
	        'label', c.label, 'title', c.title, 'state', c.state) ORDER BY c.ordinal)
	      FROM agentrt.plan_node c WHERE c.parent_plan_node_id = n.plan_node_id), '[]'::jsonb),
	    'tasks', '[]'::jsonb,
	    'bindings', COALESCE((SELECT jsonb_agg(jsonb_build_object('kind', b.bound_kind, 'ref', b.bound_ref) ORDER BY b.plan_node_binding_id)
	      FROM agentrt.plan_node_binding b WHERE b.plan_node_id = n.plan_node_id), '[]'::jsonb),
	    'events', COALESCE((SELECT jsonb_agg(jsonb_build_object('event_id', e.event_id, 'event_type', e.event_type,
	        'actor_id', e.actor_id, 'at', e.at, 'body', e.body_json) ORDER BY e.event_id)
	      FROM (SELECT * FROM agentrt.event WHERE plan_node_id = n.plan_node_id ORDER BY event_id DESC LIMIT 50) e), '[]'::jsonb)) || `+signoffs+`
	  FROM agentrt.plan_node n WHERE n.plan_node_id = $1`, chi.URLParam(r, "id"))
}

type planProposalBody struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	Root           string                 `json:"root"`
	Reason         string                 `json:"reason"`
	Changes        []planstore.PlanChange `json:"changes"`
}

// HandleProposePlanChange is POST /plan/proposals: plan_change_proposed; within
// the standing authorization decided and applied at once, otherwise tried,
// rolled back, and put to the Owner as a decision card.
func (rt Routes) HandleProposePlanChange(w http.ResponseWriter, r *http.Request) {
	var b planProposalBody
	if !rt.Host.DecodeCanonical(w, r, &b) {
		return
	}
	who, err := rt.Host.Writer(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	out, err := planstore.ProposePlanChange(r.Context(), rt.Host.DB(), who, b.IdempotencyKey, b.Root, b.Reason, b.Changes, rt.Host.Ask())
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, out)
}

type planNoteBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	Text           string `json:"text"`
}

// HandlePostPlanNote is POST /plan/{id}/notes: what an architecture row actually
// is now, in words (W-31 2.5 G2 A), appended as a note event on the row;
// GET /progress shows the latest beside the row's tick. A step takes one too
// (2026-11-23: Audit's conclusion on a standard, Triage's cause of a miss).
// A node there is not is 404, another kind 422. Who may
// propose a plan change may write one, recorded as the proposer is; the key
// is the writer's, as for proposals.
func (rt Routes) HandlePostPlanNote(w http.ResponseWriter, r *http.Request) {
	var b planNoteBody
	if !rt.Host.Decode(w, r, &b) {
		return
	}
	who, err := rt.Host.Writer(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	out, err := planstore.PostPlanNote(r.Context(), rt.Host.DB(), who, b.IdempotencyKey, chi.URLParam(r, "id"), b.Text)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, out)
}

type signoffBody struct {
	Evidence []string `json:"evidence"`
	Ref      string   `json:"ref"`
}

// HandleSignOffStep is POST /plan/{id}/signoff: the Owner signs a step off
// (ISSUE-3), the owner_signoff event its done rule's owner_signoff
// atom waits for; the controller's next rules pass moves the step. The admin
// role only (the "signoff" ability): the tokens agents hold (agentd, reporter,
// operator, viewer) are refused before this runs, and no MCP tool calls it.
// The body may name the ref; it must when the
// rule waits on more than one. A second press answers the first sign-off
// (already_signed) and writes nothing.
func (rt Routes) HandleSignOffStep(w http.ResponseWriter, r *http.Request) {
	var b signoffBody
	if !rt.Host.Decode(w, r, &b) {
		return
	}
	who, err := rt.Host.Writer(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	out, err := planstore.SignOffStep(r.Context(), rt.Host.DB(), who.Actor, chi.URLParam(r, "id"), b.Ref, b.Evidence...)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, out)
}

// HandleProgress is GET /progress: the projection of
// SPEC-agentrt-progress-2026-10-14.md, computed on every read. A read that
// fails is 503, never an old copy (review 181 B1).
func (rt Routes) HandleProgress(w http.ResponseWriter, r *http.Request) {
	out, err := planstore.ProgressSince(r.Context(), rt.Host.DB(), rt.Host.Back())
	if err != nil {
		// Unknown, never an old copy or half of one (SPEC §1).
		rt.Host.Logf("agentrt progress: %v", err)
		rt.Host.Error(w, http.StatusServiceUnavailable, "unavailable", "the runtime's state cannot be read now")
		return
	}
	rt.Host.JSON(w, http.StatusOK, out)
}
